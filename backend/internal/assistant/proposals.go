package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// Proposals are where the yes lives.
//
// The uncontained tier is never performed by the assistant and never refused
// either: asking for one of its verbs creates a proposal, a person decides it
// on a surface that can show the card or read the target back, and accepting
// RE-CHECKS the live facts before the same service the UI uses performs it.
// That re-check is the storage page's rule one layer up — the server re-plans
// and intersects with the request, so a stale card narrows what happens and
// never widens it.
//
// Three properties hold the whole design up, and each has one place in this
// file:
//
//   - The tier is on the verb. A handler here can only ever WRITE A ROW; the
//     executor lives behind [Actions], which the server implements over the
//     same services the WS ops call, and nothing reaches it except
//     [Service.Decide] on an explicit yes.
//   - One check per verb, used at proposal time and at accept time
//     ([proposalChecks]). Two copies of "is this safe" is how a card offers
//     something the executor then refuses, or worse, the other way round.
//   - Every decision is a row, a journal entry and a push. What was asked, what
//     it was judged on, who decided it where, and what happened.

// ProposalStatus is where a proposal has got to. The set is closed: a surface
// renders one card per status and a status nothing renders is a card nobody can
// act on.
type ProposalStatus string

const (
	// ProposalOpen — waiting on a person. The only status that can be decided.
	ProposalOpen ProposalStatus = "open"
	// ProposalAccepted — somebody said yes and the action was performed.
	ProposalAccepted ProposalStatus = "accepted"
	// ProposalDeclined — somebody said no. Nothing was performed.
	ProposalDeclined ProposalStatus = "declined"
	// ProposalStale — accepted, but the facts had moved and no longer allowed
	// it. Nothing was performed and [Proposal.Outcome] says what changed.
	ProposalStale ProposalStatus = "stale"
	// ProposalFailed — accepted and attempted, and the executor said no:
	// a conflict, a rebase needed, a dirty worktree, an error.
	ProposalFailed ProposalStatus = "failed"
	// ProposalExpired — nobody decided it in time.
	ProposalExpired ProposalStatus = "expired"
)

const (
	// proposalTTL is how long an undecided proposal stays an offer.
	//
	// A week, which is longer than a break and shorter than a memory: the facts
	// a proposal was judged on go stale long before this, and the accept-time
	// re-check is what covers that — the TTL only stops a card from sitting in
	// front of somebody forever. Expiry is LAZY (see [Service.expireProposals]):
	// a proposal nobody looks at costs nothing, and a sweep would be a second
	// writer on a table whose whole content is decisions.
	proposalTTL = 7 * 24 * time.Hour
	// maxProposalPage bounds one list. Open proposals are a claim on attention
	// and a surface that lists fifty of them has already failed to report.
	maxProposalPage = 50
	// decideBudget bounds one decision, executor and record together.
	//
	// It is not a responsiveness figure — it is the outer limit on how long a
	// decision may hold [Service.decideMu] once the caller has been detached
	// from it. Generous, because the slowest verb is a delete that recurses
	// into a lead's workers and removes a worktree per child, and a timeout
	// that fires in the middle of one buys nothing: the action is already
	// under way and the only thing left to lose is the row that records it.
	decideBudget = 10 * time.Minute
)

// Proposal is one thing the assistant would like done and cannot do.
//
// Every field is optional on the wire, as every field here is: the generated
// Zod schema mirrors these tags, and a required field makes a client reject the
// whole payload from a peer that does not send it.
type Proposal struct {
	ID        string `json:"id,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	// Verb is one of the eight uncontained verbs.
	Verb string `json:"verb,omitempty"`
	// SessionID is the target for every verb but dissolve_channel.
	SessionID string `json:"sessionId,omitempty"`
	// SessionName and ProjectName are RESOLVED at read time rather than stored:
	// they are presentation, and a name stored at proposal time would lag a
	// rename. A target that no longer exists — the session an accepted delete
	// removed — leaves them empty, and the journal entry the decision wrote is
	// where that name is kept for good.
	SessionName string `json:"sessionName,omitempty"`
	ProjectID   string `json:"projectId,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
	// ChannelID is the target for dissolve_channel.
	ChannelID string `json:"channelId,omitempty"`
	// Args is the verb's own arguments, already validated.
	Args map[string]any `json:"args,omitempty"`
	// Rationale is why, in the assistant's words. Required at proposal time: a
	// card with no reason on it is a button nobody can judge.
	Rationale string `json:"rationale,omitempty"`
	// Evidence is the server facts this was judged on, named so a card can
	// quote them.
	Evidence map[string]any `json:"evidence,omitempty"`
	Status   ProposalStatus `json:"status,omitempty"`
	// DecidedAt, DecidedVia and Outcome are the decision: when, on which
	// surface, and what happened.
	DecidedAt  string `json:"decidedAt,omitempty"`
	DecidedVia string `json:"decidedVia,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
}

// EventProposal carries a [Proposal] on create and on decide, on the global
// topic like every other assistant push.
const EventProposal = "assistant.proposal"

// BranchFacts is what git says about a session's branch right now.
//
// Read FRESH rather than from `branchStatusCache`: that cache has a 60-second
// TTL and only covers what nothing reports, where a proposal's whole claim is
// that it was judged on the facts as they are. Five subprocesses is the right
// price for a card somebody is about to press.
type BranchFacts struct {
	// Ahead is how many commits the branch has that the project's HEAD lacks.
	Ahead int `json:"ahead"`
	// Behind is how many the project's HEAD has that the branch lacks. Being
	// behind is what makes a merge impossible here: the server's merge is
	// fast-forward only.
	Behind int `json:"behind"`
	// Dirty says the worktree has uncommitted changes. Not a blocker for either
	// git verb — both auto-commit server-side — but it is a fact worth showing.
	Dirty bool `json:"dirty"`
	// MergeStatus is git's own word: "clean", "conflicts" or "unknown".
	MergeStatus string `json:"mergeStatus"`
	// Busy says a turn is in flight.
	Busy bool `json:"busy"`
}

// MergeStatusConflicts is the one merge status this package reasons about. The
// others it only repeats.
const MergeStatusConflicts = "conflicts"

// DeleteVerdict is the storage subsystem's judgement about one session.
//
// The two booleans rather than the verdict string, because the closed set of
// verdicts lives in internal/storage and this package does not import it: a
// closed set spelled twice is how one surface offers what the other refuses.
// Safety and Reason are carried through for the card to quote.
type DeleteVerdict struct {
	// Safe is storage's DeleteSafe and nothing else. Unknown is not safe.
	Safe bool `json:"safe"`
	// Reclaimable says the reversible verb is allowed: finished, not held by
	// the runtime, nothing uncommitted.
	Reclaimable bool `json:"reclaimable"`
	// Safety is the verdict's own word, for a card.
	Safety string `json:"safety"`
	// Reason is the phrase storage puts in front of a blocked delete.
	Reason string `json:"reason,omitempty"`
}

// ChannelFacts is what a channel looks like before it is dissolved.
type ChannelFacts struct {
	// Name is what the channel is called, for a card and a refusal.
	Name string `json:"name,omitempty"`
	// Members is how many sessions are in it.
	Members int `json:"members"`
	// Busy is how many of them are running a turn.
	Busy int `json:"busy"`
}

// SessionSettings are the two values the set_* verbs change, plus what the
// target can be asked to change at all.
//
// Live matters because both setters answer session.ErrNotLive against a session
// whose process is gone, and a proposal that can only fail is worse than a
// refusal: the operator presses it, nothing happens, and the card says so
// afterwards. The capability bits are the same rule one step earlier, and the
// stronger case: codex's adapter implements none of the three, so a card
// offering one of them could only fail — or, worse, succeed at persisting a
// claude slug into a session that cannot run it.
type SessionSettings struct {
	Model string `json:"model,omitempty"`
	Mode  string `json:"mode,omitempty"`
	Live  bool   `json:"live"`
	// Provider is the CLI family the session runs — "claude", "codex" — and is
	// here to be NAMED in a refusal, never branched on. The three bits below
	// are what decide, because a provider list spelled a second time in this
	// package is how one surface offers what another hides.
	Provider string `json:"provider,omitempty"`
	// ModelSwitch, PlanMode and AcceptEditsMode are the adapter's own verdicts,
	// read from the one table the session's `capabilities` wire field carries
	// and the composer's controls are gated on
	// (session.CapabilitiesForProvider). A model the target cannot be switched
	// to is also the catalog the spoken name is resolved against, which is why
	// [Actions.ResolveModel] takes a provider.
	ModelSwitch     bool `json:"modelSwitch"`
	PlanMode        bool `json:"planMode"`
	AcceptEditsMode bool `json:"acceptEditsMode"`
}

// ModelChoice is one model the catalog recognised: what to run, and what to
// call it.
type ModelChoice struct {
	ID    string
	Label string
}

// Actions is the uncontained tier's executor and its facts.
//
// A seam, not a convenience: this package must stay independent of the session
// pipeline, so the assistant asks in its own vocabulary and the server answers
// over the same services the WS ops call — one route into a merge, whether the
// gesture was a click or a yes on a card.
//
// **Nil is valid**, and it is the whole of what a half-wired assistant does
// here: with no Actions the uncontained verbs refuse in words and no proposal
// is written, because a proposal is a claim that the facts were checked and
// nothing can check them.
//
// Every method may be called twice for one proposal — once when the card is
// written and once when it is accepted — so none of them may have a side
// effect of its own.
type Actions interface {
	// Merge merges the session's branch into the project's, fast-forward only,
	// and answers one line about what happened. A status git refused with
	// (conflict, needs_rebase, dirty_worktree) is an error carrying that word
	// as its outcome (see [OutcomeError]), never a crash.
	Merge(ctx context.Context, sessionID string) (string, error)
	// Rebase replays the branch onto the project's HEAD, same rules.
	Rebase(ctx context.Context, sessionID string) (string, error)
	// Archive files the session away. Refused while a turn is in flight.
	Archive(ctx context.Context, sessionID string) error
	// Delete removes the session, its worktree and its branch, recursing into
	// its children.
	Delete(ctx context.Context, sessionID string) error
	// Reclaim frees the session's disk and keeps its row and branch, answering
	// one line about what was freed or why nothing was.
	Reclaim(ctx context.Context, sessionID string) (string, error)
	// Dissolve removes a channel's workers, their worktrees and their branches.
	// keepHistory keeps the channel row and the lead's link.
	Dissolve(ctx context.Context, channelID string, keepHistory bool) error
	// SetModel changes which model a live session runs.
	SetModel(ctx context.Context, sessionID, model string) error
	// SetMode changes a live session's permission mode.
	SetMode(ctx context.Context, sessionID, mode string) error

	// BranchFacts reads the branch state fresh.
	BranchFacts(ctx context.Context, sessionID string) (BranchFacts, error)
	// DeleteVerdict asks storage whether this session may be deleted or
	// reclaimed. It fails closed: anything git cannot answer is not safe.
	DeleteVerdict(ctx context.Context, sessionID string) (DeleteVerdict, error)
	// Busy reports whether a turn is in flight, from the runtime's own turn
	// lifecycle rather than from a session state.
	Busy(ctx context.Context, sessionID string) bool
	// ChannelBusy counts a channel's members and how many of them are working.
	// An error means the channel is not one this server holds.
	ChannelBusy(ctx context.Context, channelID string) (ChannelFacts, error)
	// SessionSettings reads the current model, the current permission mode,
	// whether the session is live, and which of the two set_* verbs its
	// provider implements at all.
	SessionSettings(ctx context.Context, sessionID string) (SessionSettings, error)
	// ResolveModel turns a spoken family name into a model the catalog knows
	// FOR THAT PROVIDER. An unrecognised name answers [*UnknownModelError],
	// which names the families that do exist there. The provider is a
	// parameter because the target of set_session_model is an existing session
	// whose provider is knowable, where resolving against claude's catalog
	// would hand a codex session a slug it cannot run.
	ResolveModel(ctx context.Context, provider, spoken string) (ModelChoice, error)
}

// SessionModelResolver is implemented by an [Actions] that resolves a model
// against the catalog of the machine a session runs on, rather than this one's.
type SessionModelResolver interface {
	ResolveModelFor(ctx context.Context, sessionID, provider, spoken string) (ModelChoice, error)
}

// IsSessionProposalVerb reports whether verb is an uncontained verb whose
// target is a session — the ones a paired machine can be asked to perform.
func IsSessionProposalVerb(verb string) bool {
	check, ok := proposalChecks[verb]
	return ok && check.subject == subjectSession
}

// CheckProposal runs one session verb's check against actions: the facts it
// reads, and a refusal in words when they do not allow it.
//
// Exported for the OWNER of a session on another machine (docs/peers.md): the
// acting server's card was judged on facts read over the wire, and the machine
// that bears the consequences judges them again, with the same function, before
// anything happens.
func CheckProposal(ctx context.Context, actions Actions, verb, sessionID string, args map[string]any) (map[string]any, string, error) {
	check, ok := proposalChecks[verb]
	if !ok || check.subject != subjectSession {
		return nil, "", fmt.Errorf("assistant: %q is not a session proposal verb", verb)
	}
	return check.check(ctx, ownerService(actions), Proposal{Verb: verb, SessionID: sessionID, Args: args})
}

// PerformProposal re-checks one session verb against actions and performs it
// when the facts still allow. A refusal comes back as an [*OutcomeError] whose
// outcome is the refusal, which is what a stale card shows.
func PerformProposal(ctx context.Context, actions Actions, verb, sessionID string, args map[string]any) (string, error) {
	check, ok := proposalChecks[verb]
	if !ok || check.subject != subjectSession {
		return "", fmt.Errorf("assistant: %q is not a session proposal verb", verb)
	}
	svc := ownerService(actions)
	p := Proposal{Verb: verb, SessionID: sessionID, Args: args}
	if _, refusal, err := check.check(ctx, svc, p); err != nil {
		return "", err
	} else if refusal != "" {
		return "", &OutcomeError{Outcome: refusal, Detail: "refused by the owner's re-check"}
	}
	return check.exec(ctx, svc, p)
}

// ownerService is the part of a Service the checks and executors read: the
// actions and a logger. Nothing else is touched by them.
func ownerService(actions Actions) *Service {
	return &Service{actions: actions, log: slog.Default()}
}

// WithActions gives the assistant the uncontained tier's executor.
//
// Nil is valid: without it the eight verbs are still in the table — a head has
// to know they exist — and each answers in words that it cannot be checked
// here, rather than writing a proposal nothing verified.
func WithActions(a Actions) Option { return func(s *Service) { s.actions = a } }

// OutcomeError is an executor failure whose own word is what a card shows.
//
// A merge that comes back `needs_rebase` did not break: git answered, and the
// answer is the outcome. The alternative was the server mapping statuses to
// prose and this package mapping prose back, which is two vocabularies for one
// fact.
type OutcomeError struct {
	// Outcome is the one line a card and the journal print.
	Outcome string
	// Detail is for the log, and never for a surface.
	Detail string
}

func (e *OutcomeError) Error() string {
	if e.Detail == "" {
		return e.Outcome
	}
	return e.Outcome + ": " + e.Detail
}

// Permission modes, the closed set `session.set-permission` accepts.
//
// Spelled here because the setter underneath COERCES an unknown mode to
// "default" rather than refusing it — so a proposal that did not validate would
// be accepted, perform something else, and report success.
const (
	PermissionModeDefault     = "default"
	PermissionModePlan        = "plan"
	PermissionModeAcceptEdits = "acceptEdits"
)

func permissionModes() []string {
	return []string{PermissionModeDefault, PermissionModePlan, PermissionModeAcceptEdits}
}

// subjectKind says what a verb's target is.
type subjectKind string

const (
	subjectSession subjectKind = "session"
	subjectChannel subjectKind = "channel"
)

// verbCheck is one uncontained verb's rules: what it is called, what it aims
// at, what has to be true, and how it is performed.
//
// check is the one function used at proposal time and at accept time. It reads
// the facts and answers them, plus a refusal in plain words when they do not
// allow the action — "" means they do. At proposal time a refusal is the whole
// answer and no row is written; at accept time it is [ProposalStale] with the
// refusal as the outcome, and nothing is performed.
type verbCheck struct {
	// words is the verb in words, for a card, a refusal and a journal line.
	words   string
	subject subjectKind
	// prepare validates the verb's own arguments and writes back what it
	// resolved. A refusal payload means nothing was proposed.
	prepare func(ctx context.Context, s *Service, p *Proposal) map[string]any
	check   func(ctx context.Context, s *Service, p Proposal) (map[string]any, string, error)
	exec    func(ctx context.Context, s *Service, p Proposal) (string, error)
}

// proposalChecks is the table, keyed by verb. Every uncontained verb has an
// entry and nothing else does: the tier is a property of the verb, so the
// mapping from verb to "this needs a yes" is a lookup rather than a judgement
// at a call site.
var proposalChecks = map[string]verbCheck{
	VerbMergeSession: {
		words:   "Merge this session's branch into the project's",
		subject: subjectSession,
		check:   checkMerge,
		exec:    execMerge,
	},
	VerbRebaseSession: {
		words:   "Rebase this session's branch onto the project's",
		subject: subjectSession,
		check:   checkRebase,
		exec:    execRebase,
	},
	VerbArchiveSession: {
		words:   "Archive this session",
		subject: subjectSession,
		check:   checkArchive,
		exec:    execArchive,
	},
	VerbDeleteSession: {
		words:   "Delete this session, its worktree and its branch",
		subject: subjectSession,
		check:   checkDelete,
		exec:    execDelete,
	},
	VerbReclaimSession: {
		words:   "Reclaim this session's disk, keeping its row and its branch",
		subject: subjectSession,
		check:   checkReclaim,
		exec:    execReclaim,
	},
	VerbDissolveChannel: {
		words:   "Dissolve this channel and remove its workers",
		subject: subjectChannel,
		prepare: prepareDissolve,
		check:   checkDissolve,
		exec:    execDissolve,
	},
	VerbSetSessionModel: {
		words:   "Change which model this session runs",
		subject: subjectSession,
		prepare: prepareSetModel,
		check:   checkSetModel,
		exec:    execSetModel,
	},
	VerbSetSessionMode: {
		words:   "Change this session's permission mode",
		subject: subjectSession,
		prepare: prepareSetMode,
		check:   checkSetMode,
		exec:    execSetMode,
	},
}

// proposalParams is the argument list every uncontained verb declares.
//
// rationale is on every one of them and is required, because the card is read
// by somebody who was not in the conversation. The target argument differs, so
// it is passed in.
func proposalParams(target Param) []Param {
	return []Param{
		target,
		{
			Name: "rationale", Type: ParamString, Required: true,
			Description: "Why this should happen, in one line, written for them rather than for you. " +
				"It goes on the card they press, and a card with no reason on it is one they cannot " +
				"judge.",
		},
	}
}

var sessionTarget = Param{
	Name: "session_id", Type: ParamString, Required: true,
	Description: "The session id exactly as it was returned to you.",
}

// uncontainedVerbs is the eight, with their handlers.
//
// A handler here CREATES A PROPOSAL and can do nothing else: that is what makes
// the tier a property of the table rather than of a prompt. The description
// says so in the words the head reads, because a verb that looks performable is
// one it will promise.
func (s *Service) uncontainedVerbs() []Verb {
	return []Verb{
		{
			Name:    VerbMergeSession,
			Tier:    TierUncontained,
			Input:   proposalParams(sessionTarget),
			handler: s.proposeVerb(VerbMergeSession),
			Description: "Propose merging a session's branch into the project's. You do not merge: " +
				"this puts a card in front of them and they decide it." + proposalNote,
		},
		{
			Name:    VerbRebaseSession,
			Tier:    TierUncontained,
			Input:   proposalParams(sessionTarget),
			handler: s.proposeVerb(VerbRebaseSession),
			Description: "Propose rebasing a session's branch onto the project's, which is what a " +
				"branch that is behind needs before it can merge." + proposalNote,
		},
		{
			Name:    VerbArchiveSession,
			Tier:    TierUncontained,
			Input:   proposalParams(sessionTarget),
			handler: s.proposeVerb(VerbArchiveSession),
			Description: "Propose filing a session away. Reversible, and still theirs to decide: " +
				"archiving is their own gesture." + proposalNote,
		},
		{
			Name:    VerbDeleteSession,
			Tier:    TierUncontained,
			Input:   proposalParams(sessionTarget),
			handler: s.proposeVerb(VerbDeleteSession),
			Description: "Propose deleting a session, its worktree and its branch. Irreversible, and " +
				"only offered when git says the commits are already on the project's main line." +
				proposalNote,
		},
		{
			Name:    VerbReclaimSession,
			Tier:    TierUncontained,
			Input:   proposalParams(sessionTarget),
			handler: s.proposeVerb(VerbReclaimSession),
			Description: "Propose freeing a finished session's disk. The row and the branch stay, and " +
				"the next message re-provisions the worktree." + proposalNote,
		},
		{
			Name:    VerbDissolveChannel,
			Tier:    TierUncontained,
			handler: s.proposeVerb(VerbDissolveChannel),
			Input: append(proposalParams(Param{
				Name: "channel_id", Type: ParamString, Required: true,
				Description: "The channel id exactly as it was returned to you.",
			}), Param{
				Name: "keep_history", Type: ParamBoolean,
				Description: "true (the default) keeps the channel and its timeline as a read-only " +
					"record. false deletes the channel row as well.",
			}),
			Description: "Propose dissolving a channel: its workers stop and their worktrees and " +
				"branches go." + proposalNote,
		},
		{
			Name:    VerbSetSessionModel,
			Tier:    TierUncontained,
			handler: s.proposeVerb(VerbSetSessionModel),
			Input: append(proposalParams(sessionTarget), Param{
				Name: "model", Type: ParamString, Required: true,
				Description: "The model family they asked for -- \"fable\", \"opus\", \"sonnet\", " +
					"\"haiku\". Never a version number and never a model id.",
			}),
			Description: "Propose changing which model another session runs." + proposalNote,
		},
		{
			Name:    VerbSetSessionMode,
			Tier:    TierUncontained,
			handler: s.proposeVerb(VerbSetSessionMode),
			Input: append(proposalParams(sessionTarget), Param{
				Name: "mode", Type: ParamString, Required: true,
				Description: "default: it asks before acting. plan: it plans and changes nothing. " +
					"acceptEdits: it edits files without asking.",
				Enum: permissionModes(),
			}),
			Description: "Propose changing another session's permission mode." + proposalNote,
		},
	}
}

// proposalNote is the sentence every uncontained verb's description ends with.
// One string, because eight verbs saying it eight ways is eight chances for one
// of them to read as performable.
const proposalNote = " Nothing happens until they accept it. Tell them in one line what you " +
	"proposed and that it is waiting for them; do not say it is done."

// proposeVerb is the handler every uncontained verb carries.
func (s *Service) proposeVerb(verb string) Handler {
	return func(ctx context.Context, args map[string]any) (map[string]any, error) {
		return s.propose(ctx, verb, args)
	}
}

// propose writes one proposal, or refuses.
//
// The order is deliberate. The target is resolved and the arguments validated
// first, because a refusal about those costs nothing; an existing open proposal
// short-circuits next, because asking twice is one card and re-reading the
// facts for a duplicate is five git subprocesses nobody asked for; and only
// then are the facts gathered and judged.
//
// A refusal is not a proposal. When the evidence already says no — a merge of a
// branch that is behind — the answer is "rebase first" in words, not a card
// that could only fail.
func (s *Service) propose(ctx context.Context, verb string, args map[string]any) (map[string]any, error) {
	check, known := proposalChecks[verb]
	if !known {
		return nil, fmt.Errorf("%q is uncontained and has no proposal check", verb)
	}

	if s.actions == nil {
		return refuse("no-actions:"+verb, fmt.Sprintf("NOTHING WAS PROPOSED: I cannot check whether "+
			"that would be safe from here, and I will not put something in front of them that nothing "+
			"has checked. Say plainly that %s has to be done on screen.",
			strings.ToLower(check.words))), nil
	}

	rationale := strings.TrimSpace(stringArg(args, "rationale"))
	if rationale == "" {
		return refuse("no-rationale:"+verb, "NOTHING WAS PROPOSED: there was no reason with it, and a "+
			"card with no reason on it is a button they cannot judge. Say in one line why this should "+
			"happen and ask again."), nil
	}

	p := Proposal{Verb: verb, Rationale: rationale, Args: s.declaredArgs(verb, args)}
	if refusal := s.resolveTarget(ctx, check, &p, args); refusal != nil {
		return refusal, nil
	}
	if check.prepare != nil {
		if refusal := check.prepare(ctx, s, &p); refusal != nil {
			return refusal, nil
		}
	}

	existing, found, err := s.openProposalFor(ctx, verb, p.SessionID, p.ChannelID)
	if err != nil {
		s.log.Warn("assistant: open proposal check failed", "verb", verb, "session", p.SessionID,
			"channel", p.ChannelID, "error", err)
		return refuse("duplicate-check-failed:"+verb, "NOTHING WAS PROPOSED: I could not check whether "+
			"that is already waiting for them, and two cards for one decision is two buttons for one "+
			"yes. Say so plainly and ask again in a moment."), nil
	}
	if found {
		return alreadyOpenAnswer(existing), nil
	}

	evidence, refusal, err := check.check(ctx, s, p)
	if err != nil {
		s.log.Warn("assistant: proposal facts unavailable", "verb", verb, "session", p.SessionID,
			"channel", p.ChannelID, "error", err)
		return refuse("facts-unavailable:"+verb, "NOTHING WAS PROPOSED: I could not read the facts "+
			"that would have to be true for that, so there is nothing to put in front of them. Say so "+
			"plainly."), nil
	}
	if refusal != "" {
		return refuse("evidence-refuses:"+verb, fmt.Sprintf("NOTHING WAS PROPOSED, because %s. Say "+
			"that plainly as the answer -- it is the useful thing to know, not a failure.", refusal)), nil
	}
	if machine, ok := p.Args["machine"].(string); ok && machine != "" {
		if evidence == nil {
			evidence = map[string]any{}
		}
		evidence["machine"] = machine
	}
	p.Evidence = evidence

	at := formatTime(s.now())
	row, err := s.store.InsertAssistantProposal(ctx, store.InsertAssistantProposalParams{
		ID:        uuid.New().String(),
		CreatedAt: at,
		Verb:      verb,
		SessionID: p.SessionID,
		ProjectID: p.ProjectID,
		ChannelID: p.ChannelID,
		Args:      encodeJSONObject(s, "proposal args", p.Args),
		Rationale: rationale,
		Evidence:  encodeJSONObject(s, "proposal evidence", evidence),
		ExpiresAt: formatTime(s.now().Add(proposalTTL)),
	})
	if err != nil {
		// One open row per verb and target is a UNIQUE INDEX (migration 058), so
		// this is also where a lost race lands: two asks for the same thing both
		// read no open row above, and the second insert is refused. Asking the
		// question again rather than reading the driver's error code answers both
		// causes with the one thing that matters — if there is an open card now,
		// it IS the card, whoever wrote it.
		if existing, found, reread := s.openProposalFor(ctx, verb, p.SessionID, p.ChannelID); reread == nil && found {
			return alreadyOpenAnswer(existing), nil
		}
		return nil, fmt.Errorf("write proposal %s: %w", verb, err)
	}

	proposal := s.proposalFrom(ctx, row)
	s.announceProposal(ctx, proposal)
	if _, err := s.appendJournal(ctx, journalWrite{
		Kind:      JournalProposalMade,
		SessionID: proposal.SessionID,
		ProjectID: proposal.ProjectID,
		Summary:   fmt.Sprintf("proposed: %s -- %s", proposalSubjectLine(proposal), rationale),
		Payload: map[string]any{
			"proposalId": proposal.ID,
			"verb":       verb,
			"name":       proposal.SessionName,
			"evidence":   evidence,
		},
	}); err != nil {
		s.log.Warn("assistant: proposal not journaled", "proposal", proposal.ID, "error", err)
	}

	return map[string]any{
		"proposal_id": proposal.ID,
		"status":      string(proposal.Status),
		"note": fmt.Sprintf("PROPOSED, NOT DONE: %s is waiting for them to accept it, on the thread "+
			"or on a call. Tell them in one line what it would do and that it is theirs to decide. "+
			"You have no way to accept it yourself.", proposalSubjectLine(proposal)),
	}, nil
}

// declaredArgs copies the verb's own arguments out of what a head sent.
//
// Declared ones only, so a model that sent a field nobody asked for cannot put
// it in the row, and the target and the rationale left out, because both have
// a column of their own — a second copy in `args` is a second thing that can
// disagree about the same proposal.
func (s *Service) declaredArgs(verb string, args map[string]any) map[string]any {
	out := map[string]any{}
	declared, known := s.byName[verb]
	if !known {
		return out
	}
	for _, param := range declared.Input {
		switch param.Name {
		case "rationale", "session_id", "channel_id":
			continue
		}
		value, present := args[param.Name]
		if !present {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				out[param.Name] = trimmed
			}
		case bool:
			out[param.Name] = typed
		case float64:
			out[param.Name] = typed
		}
	}
	return out
}

// resolveTarget works out what a proposal is about and refuses what cannot be
// aimed at from here.
//
// A session has to be one this machine owns, for the same reason dispatch does:
// the world snapshot a remote row comes from is a view, and a view can make the
// assistant say things, never do things. A channel has to be one this server
// holds, which [Actions.ChannelBusy] answers by failing.
func (s *Service) resolveTarget(ctx context.Context, check verbCheck, p *Proposal, args map[string]any) map[string]any {
	if check.subject == subjectChannel {
		channelID := strings.TrimSpace(stringArg(args, "channel_id"))
		if channelID == "" {
			return refuse("no-channel-id", "NOTHING WAS PROPOSED: no channel id. Find the channel "+
				"first and use the id it came back with.")
		}
		p.ChannelID = channelID
		return nil
	}

	sessionID := strings.TrimSpace(stringArg(args, "session_id"))
	if sessionID == "" {
		return refuse("no-session-id", "NOTHING WAS PROPOSED: no session id. Find the session first "+
			"and use the id it came back with.")
	}
	if s.dir == nil {
		return refuse("no-directory", "NOTHING WAS PROPOSED: I cannot tell which session that is from "+
			"here. Tell them it has to be done on screen.")
	}
	// Wherever it runs (docs/peers.md). A paired machine's session is judged on
	// that machine's facts, read over its peer surface, and performed there —
	// where the owner re-checks before it executes.
	row, found := s.locate(ctx, sessionID)
	if !found {
		return refuse("target-unknown", "NOTHING WAS PROPOSED: no session with that id is known here. "+
			"Find it again and use the id that comes back.")
	}
	if !row.Reach.CanAct() {
		out := cannotAct("target", row, "changes like that")
		out["error"] = strings.Replace(out["error"].(string), "NOTHING WAS DONE", "NOTHING WAS PROPOSED", 1)
		return out
	}
	p.SessionID = row.ID
	p.ProjectID = row.ProjectID
	if row.Reach.Remote() {
		// Carried to the evidence once the check has written it: a card names
		// whose facts it was judged on.
		p.Args["machine"] = row.MachineName
	}
	return nil
}

// openProposalFor finds an open proposal for the same verb and target.
//
// Three answers, not two. A MISS is `(_, false, nil)`; anything else the store
// says is an error, because "I could not check" read as "there is none" writes
// the second card the whole rule exists to prevent — the one case where a
// locked database turns into two buttons for one decision.
func (s *Service) openProposalFor(ctx context.Context, verb, sessionID, channelID string) (Proposal, bool, error) {
	row, err := s.store.GetOpenAssistantProposalFor(ctx, store.GetOpenAssistantProposalForParams{
		Verb:      verb,
		SessionID: sessionID,
		ChannelID: channelID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, false, nil
	}
	if err != nil {
		return Proposal{}, false, fmt.Errorf("open proposal for %s: %w", verb, err)
	}
	p := s.proposalFrom(ctx, row)
	// An open row past its expiry is not an offer any more, so it is not a
	// duplicate either: proposalFrom has already read it as expired.
	if p.Status != ProposalOpen {
		return Proposal{}, false, nil
	}
	return p, true, nil
}

// alreadyOpenAnswer is what a second ask for the same verb and target gets.
//
// The card's own words come back with it. The note tells the head to say what
// it says, and one open row per verb and target means the waiting one may not
// be the one just asked for — a second model for the same session answers here,
// and "that is already waiting" without the arguments would name the wrong
// change.
func alreadyOpenAnswer(existing Proposal) map[string]any {
	answer := map[string]any{
		"proposal_id":  existing.ID,
		"status":       string(existing.Status),
		"already_open": true,
		"rationale":    existing.Rationale,
		"note": fmt.Sprintf("That is already waiting for them, proposed %s. Do not propose it "+
			"again: tell them it is there and what it says -- and if what they have just asked "+
			"for is not what the waiting one says, say that too rather than proposing a second.",
			existing.CreatedAt),
	}
	if len(existing.Args) > 0 {
		answer["args"] = existing.Args
	}
	return answer
}

// Proposals lists what has been proposed: open first, then decided, newest
// first.
//
// A pure read, which is what lets the socket op sit on the concurrent lane.
// Expiry is DERIVED here and written on the next decide: a read that writes is
// the one mistake this package has already made once, and an expired row
// rendered from a derived status looks the same to every surface.
func (s *Service) Proposals(ctx context.Context, limit int) ([]Proposal, error) {
	rows, err := s.store.ListAssistantProposals(ctx, int64(clampLimit(limit, maxProposalPage)))
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	out := make([]Proposal, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.proposalFrom(ctx, row))
	}
	return out, nil
}

// OpenProposals is the half a surface renders as cards.
func (s *Service) OpenProposals(ctx context.Context) ([]Proposal, error) {
	all, err := s.Proposals(ctx, maxProposalPage)
	if err != nil {
		return nil, err
	}
	open := make([]Proposal, 0, len(all))
	for _, p := range all {
		if p.Status == ProposalOpen {
			open = append(open, p)
		}
	}
	return open, nil
}

// Proposal reads one by id.
func (s *Service) Proposal(ctx context.Context, id string) (Proposal, error) {
	row, err := s.store.GetAssistantProposal(ctx, strings.TrimSpace(id))
	if err != nil {
		return Proposal{}, fmt.Errorf("proposal %q: %w", id, err)
	}
	return s.proposalFrom(ctx, row), nil
}

// Decide is the yes and the no.
//
// A row that is not open answers ITSELF, unchanged: two surfaces can hold the
// same card, and the second one to press must learn what happened rather than
// be told it broke something. Decline records the no and performs nothing.
// Accept re-reads the facts through the same check the card was written from —
// if they no longer allow the action the row goes [ProposalStale] with the
// reason as its outcome and nothing is performed, which is the storage page's
// rule: a stale card narrows what happens and never widens it.
//
// One decision at a time, process-wide. The status guard in SQL protects the
// row but not the executing: without the lock two accepts arriving together
// would both read `open` and both merge. Decisions are rare and the socket op
// runs off the dispatch loop, so serialising them costs nothing anybody can
// feel.
//
// A decision also OUTLIVES ITS CALLER. The request context is the socket's on
// the thread path and a 30-second tool budget on the voice one, and either can
// die while the executor works — a closed tab mid-merge, a reclaim's
// os.RemoveAll outrunning the budget. If the action lands and the write does
// not, the row stays `open`, which is the one status this function acts on: the
// next press performs it again. So the executor and the record of it are
// detached together and bounded on their own.
func (s *Service) Decide(ctx context.Context, surface, id string, accept bool) (Proposal, error) {
	if err := checkSurface(surface); err != nil {
		return Proposal{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Proposal{}, errors.New("assistant: no proposal to decide")
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), decideBudget)
	defer cancel()

	s.decideMu.Lock()
	defer s.decideMu.Unlock()

	// Lazy expiry, on the mutation side of the house: an open row past its
	// expiry becomes what it already reads as.
	s.expireProposals(ctx)

	p, err := s.Proposal(ctx, id)
	if err != nil {
		return Proposal{}, err
	}
	if p.Status != ProposalOpen {
		return p, nil
	}

	if !accept {
		return s.settle(ctx, p, surface, ProposalDeclined, ""), nil
	}

	check, known := proposalChecks[p.Verb]
	if !known || s.actions == nil {
		s.log.Warn("assistant: proposal cannot be performed here", "proposal", p.ID, "verb", p.Verb)
		return s.settle(ctx, p, surface, ProposalFailed,
			"this server cannot perform that any more"), nil
	}

	// The re-check gets a COPY: its evidence describes the facts NOW, where the
	// row's evidence is what the proposal was judged on, and overwriting that
	// would erase the comparison the card exists to make.
	_, refusal, err := check.check(ctx, s, p)
	if err != nil {
		s.log.Warn("assistant: proposal facts unavailable at accept",
			"proposal", p.ID, "verb", p.Verb, "error", err)
		return s.settle(ctx, p, surface, ProposalStale,
			"the facts it was judged on could not be read again, so nothing was done"), nil
	}
	if refusal != "" {
		return s.settle(ctx, p, surface, ProposalStale, refusal), nil
	}

	outcome, err := check.exec(ctx, s, p)
	if err != nil {
		var typed *OutcomeError
		if errors.As(err, &typed) {
			s.log.Warn("assistant: proposal failed", "proposal", p.ID, "verb", p.Verb,
				"outcome", typed.Outcome, "detail", typed.Detail)
			return s.settle(ctx, p, surface, ProposalFailed, typed.Outcome), nil
		}
		s.log.Warn("assistant: proposal failed", "proposal", p.ID, "verb", p.Verb, "error", err)
		return s.settle(ctx, p, surface, ProposalFailed, "it did not go through"), nil
	}
	if strings.TrimSpace(outcome) == "" {
		outcome = "done"
	}
	return s.settle(ctx, p, surface, ProposalAccepted, outcome), nil
}

// settle records a decision, journals it and delivers the row.
//
// The write is guarded on `status = 'open'` in SQL as well as by the lock, and
// it answers how many rows it changed rather than leaving the caller to assume
// the guard matched. Both failures are logged at Error and neither is a
// refusal: by the time this runs the action may already have happened, the row
// is the thing that is wrong, and saying nothing would hide a merge that
// landed. The returned proposal describes what was done, which is why
// [Service.Decide] detaches this from the request context — a row left `open`
// after an action is one the next press performs again.
func (s *Service) settle(ctx context.Context, p Proposal, surface string, status ProposalStatus, outcome string) Proposal {
	at := formatTime(s.now())
	changed, err := s.store.DecideAssistantProposal(ctx, store.DecideAssistantProposalParams{
		Status:     string(status),
		DecidedAt:  at,
		DecidedVia: surface,
		Outcome:    outcome,
		ID:         p.ID,
	})
	switch {
	case err != nil:
		s.log.Error("assistant: proposal decision not recorded",
			"proposal", p.ID, "status", status, "error", err)
	case changed == 0:
		// The guard did not match, so this row was not open when the write
		// arrived: somebody else settled it, or it expired underneath the
		// decision. Worth its own line, because it is the only way the row on
		// disk can disagree with what was just performed.
		s.log.Error("assistant: proposal was already settled by somebody else",
			"proposal", p.ID, "status", status, "via", surface)
	}

	p.Status = status
	p.DecidedAt = at
	p.DecidedVia = surface
	p.Outcome = outcome

	s.announceProposal(ctx, p)
	if _, err := s.appendJournal(ctx, journalWrite{
		Kind:      JournalProposalDecided,
		SessionID: p.SessionID,
		ProjectID: p.ProjectID,
		Summary:   decisionSummary(p),
		Payload: map[string]any{
			"proposalId": p.ID,
			"verb":       p.Verb,
			"status":     string(status),
			"via":        surface,
			"name":       p.SessionName,
		},
	}); err != nil {
		s.log.Warn("assistant: decision not journaled", "proposal", p.ID, "error", err)
	}
	return p
}

// announceProposal pushes the row and hands it to every surface.
//
// Both, because they answer different readers: the push is what a page already
// open re-renders from, and [ItemProposal] is what a surface that cannot
// re-render — a live call — is handed so it can say it out loud.
func (s *Service) announceProposal(ctx context.Context, p Proposal) {
	s.broadcast(EventProposal, p)
	proposal := p
	s.deliver(ctx, Item{Kind: ItemProposal, SessionID: p.SessionID, Proposal: &proposal})
}

// expireProposals applies the TTL. Best effort: a proposal that stays `open`
// in the table past its expiry still READS as expired everywhere, because
// [Service.proposalFrom] derives it.
func (s *Service) expireProposals(ctx context.Context) {
	if err := s.store.ExpireAssistantProposals(ctx, formatTime(s.now())); err != nil {
		s.log.Warn("assistant: proposals not expired", "error", err)
	}
}

// proposalFrom maps a stored row to the wire shape, resolving the target's
// names and deriving expiry.
func (s *Service) proposalFrom(ctx context.Context, row store.AssistantProposal) Proposal {
	p := Proposal{
		ID:         row.ID,
		CreatedAt:  row.CreatedAt,
		Verb:       row.Verb,
		SessionID:  row.SessionID,
		ProjectID:  row.ProjectID,
		ChannelID:  row.ChannelID,
		Args:       decodeJSONObject(row.Args),
		Rationale:  row.Rationale,
		Evidence:   decodeJSONObject(row.Evidence),
		Status:     ProposalStatus(row.Status),
		DecidedAt:  row.DecidedAt,
		DecidedVia: row.DecidedVia,
		Outcome:    row.Outcome,
		ExpiresAt:  row.ExpiresAt,
	}
	if p.Status == ProposalOpen && p.ExpiresAt != "" && p.ExpiresAt <= formatTime(s.now()) {
		p.Status = ProposalExpired
	}
	if p.SessionID != "" && s.dir != nil {
		if row, found := s.locate(ctx, p.SessionID); found {
			p.SessionName = row.Name
			p.ProjectName = row.ProjectName
			if p.ProjectName == "" {
				p.ProjectName = row.ProjectSlug
			}
			if p.ProjectID == "" {
				p.ProjectID = row.ProjectID
			}
		}
	}
	if p.ChannelID != "" {
		if name, ok := p.Args["channel"].(string); ok {
			p.SessionName = name
		}
	}
	return p
}

// proposalSubjectLine names a proposal for a sentence: the verb in words and
// the target it is about.
func proposalSubjectLine(p Proposal) string {
	words := "this"
	if check, known := proposalChecks[p.Verb]; known {
		words = check.words
	}
	target := proposalTarget(p)
	if target == "" {
		return words
	}
	return words + " (" + target + ")"
}

// proposalTarget is what the proposal is about, named the way every other
// surface names it: the session and the project it is in.
func proposalTarget(p Proposal) string {
	if p.SessionID != "" {
		return DisplayFor(SessionRow{
			ID:          p.SessionID,
			Name:        p.SessionName,
			ProjectName: p.ProjectName,
		})
	}
	if name, ok := p.Args["channel"].(string); ok && name != "" {
		return name
	}
	if p.ChannelID != "" {
		return "a channel"
	}
	return ""
}

// decisionSummary is the journal's one line for a decision.
func decisionSummary(p Proposal) string {
	var verb string
	switch p.Status {
	case ProposalAccepted:
		verb = "accepted"
	case ProposalDeclined:
		verb = "declined"
	case ProposalStale:
		verb = "went stale"
	case ProposalFailed:
		verb = "failed"
	default:
		verb = string(p.Status)
	}
	line := fmt.Sprintf("%s: %s", verb, proposalSubjectLine(p))
	if p.Outcome != "" {
		line += " -- " + p.Outcome
	}
	return line
}

// --- the checks, one per verb ---

func branchEvidence(facts BranchFacts) map[string]any {
	return map[string]any{
		"ahead":       facts.Ahead,
		"behind":      facts.Behind,
		"dirty":       facts.Dirty,
		"mergeStatus": facts.MergeStatus,
		"busy":        facts.Busy,
	}
}

func checkMerge(ctx context.Context, s *Service, p Proposal) (map[string]any, string, error) {
	facts, err := s.actions.BranchFacts(ctx, p.SessionID)
	if err != nil {
		return nil, "", fmt.Errorf("branch facts for %s: %w", p.SessionID, err)
	}
	evidence := branchEvidence(facts)
	switch {
	case facts.Busy:
		return evidence, "it is running a turn, and nothing touches a branch while one is in flight", nil
	case facts.MergeStatus == MergeStatusConflicts:
		return evidence, "the merge would conflict, and the conflicts have to be resolved in the " +
			"session before it can go in", nil
	case facts.Behind > 0:
		// The one refusal the contract names by hand. The server's merge is
		// fast-forward only, so merging a branch that is behind can only ever
		// answer needs_rebase: the useful answer is the other verb.
		return evidence, fmt.Sprintf("the branch is %s behind the project's, so it needs a rebase "+
			"first -- the merge here is fast-forward only", commits(facts.Behind)), nil
	case facts.Ahead == 0:
		return evidence, "the branch has nothing the project does not already have, so there is " +
			"nothing to merge", nil
	}
	return evidence, "", nil
}

func execMerge(ctx context.Context, s *Service, p Proposal) (string, error) {
	return s.actions.Merge(ctx, p.SessionID)
}

func checkRebase(ctx context.Context, s *Service, p Proposal) (map[string]any, string, error) {
	facts, err := s.actions.BranchFacts(ctx, p.SessionID)
	if err != nil {
		return nil, "", fmt.Errorf("branch facts for %s: %w", p.SessionID, err)
	}
	evidence := branchEvidence(facts)
	switch {
	case facts.Busy:
		return evidence, "it is running a turn, and nothing touches a branch while one is in flight", nil
	case facts.Behind == 0:
		return evidence, "the branch is already on top of the project's, so there is nothing to " +
			"rebase onto", nil
	}
	return evidence, "", nil
}

func execRebase(ctx context.Context, s *Service, p Proposal) (string, error) {
	return s.actions.Rebase(ctx, p.SessionID)
}

func checkArchive(ctx context.Context, s *Service, p Proposal) (map[string]any, string, error) {
	busy := s.actions.Busy(ctx, p.SessionID)
	evidence := map[string]any{"busy": busy}
	if busy {
		return evidence, "it is running a turn, and archiving is refused while one is in flight -- " +
			"the turn would keep running behind a row they think is filed away", nil
	}
	return evidence, "", nil
}

func execArchive(ctx context.Context, s *Service, p Proposal) (string, error) {
	if err := s.actions.Archive(ctx, p.SessionID); err != nil {
		return "", err
	}
	return "archived, and its branch and worktree are untouched", nil
}

func deleteEvidence(verdict DeleteVerdict) map[string]any {
	evidence := map[string]any{
		"safe":        verdict.Safe,
		"reclaimable": verdict.Reclaimable,
		"safety":      verdict.Safety,
	}
	if verdict.Reason != "" {
		evidence["reason"] = verdict.Reason
	}
	return evidence
}

func checkDelete(ctx context.Context, s *Service, p Proposal) (map[string]any, string, error) {
	verdict, err := s.actions.DeleteVerdict(ctx, p.SessionID)
	if err != nil {
		return nil, "", fmt.Errorf("delete verdict for %s: %w", p.SessionID, err)
	}
	evidence := deleteEvidence(verdict)
	if !verdict.Safe {
		reason := verdict.Reason
		if reason == "" {
			reason = "could not be checked against the main branch"
		}
		return evidence, fmt.Sprintf("git says it %s, and deleting is irreversible -- so that is not "+
			"something to offer", reason), nil
	}
	return evidence, "", nil
}

func execDelete(ctx context.Context, s *Service, p Proposal) (string, error) {
	if err := s.actions.Delete(ctx, p.SessionID); err != nil {
		return "", err
	}
	return "deleted, with its worktree and its branch", nil
}

func checkReclaim(ctx context.Context, s *Service, p Proposal) (map[string]any, string, error) {
	verdict, err := s.actions.DeleteVerdict(ctx, p.SessionID)
	if err != nil {
		return nil, "", fmt.Errorf("storage verdict for %s: %w", p.SessionID, err)
	}
	evidence := deleteEvidence(verdict)
	if !verdict.Reclaimable {
		reason := verdict.Reason
		if reason == "" {
			reason = "is not finished and clean"
		}
		return evidence, fmt.Sprintf("it %s, and reclaiming only applies to a session that is "+
			"finished with nothing uncommitted in it", reason), nil
	}
	return evidence, "", nil
}

func execReclaim(ctx context.Context, s *Service, p Proposal) (string, error) {
	return s.actions.Reclaim(ctx, p.SessionID)
}

// prepareDissolve reads the channel's own facts once, so the card can name the
// channel rather than an id.
func prepareDissolve(ctx context.Context, s *Service, p *Proposal) map[string]any {
	facts, err := s.actions.ChannelBusy(ctx, p.ChannelID)
	if err != nil {
		return refuse("channel-not-found", "NOTHING WAS PROPOSED: that is not a channel on this "+
			"machine. Say so plainly rather than guessing at which one they meant.")
	}
	if facts.Name != "" {
		p.Args["channel"] = facts.Name
	}
	p.Args["keep_history"] = keepHistoryArg(p.Args)
	return nil
}

// keepHistoryArg reads dissolve's one argument, with its default.
//
// Keep-history is the default, because it is the half of dissolve that keeps a
// record: the workers go either way, and losing the timeline as well is a
// second decision nobody asked for. Spelled ONCE, because prepare writes the
// value and exec reads it back out of a JSON column — and a column that does
// not round-trip must not turn "keep the record" into a full delete. Every
// verdict in this subsystem fails closed; for this one, closed is true.
func keepHistoryArg(args map[string]any) bool {
	keep, said := args["keep_history"].(bool)
	if !said {
		return true
	}
	return keep
}

func checkDissolve(ctx context.Context, s *Service, p Proposal) (map[string]any, string, error) {
	facts, err := s.actions.ChannelBusy(ctx, p.ChannelID)
	if err != nil {
		return nil, "", fmt.Errorf("channel facts for %s: %w", p.ChannelID, err)
	}
	evidence := map[string]any{"members": facts.Members, "busy": facts.Busy}
	if facts.Name != "" {
		evidence["channel"] = facts.Name
	}
	if facts.Busy > 0 {
		return evidence, fmt.Sprintf("%d of its %d members are still working, and dissolving would "+
			"take their worktrees out from under them", facts.Busy, facts.Members), nil
	}
	return evidence, "", nil
}

func execDissolve(ctx context.Context, s *Service, p Proposal) (string, error) {
	keep := keepHistoryArg(p.Args)
	if err := s.actions.Dissolve(ctx, p.ChannelID, keep); err != nil {
		return "", err
	}
	if keep {
		return "dissolved: its workers, worktrees and branches are gone, and the channel stays as a " +
			"read-only record", nil
	}
	return "dissolved: its workers, worktrees and branches are gone, and so is the channel", nil
}

// prepareSetModel resolves the spoken family name through the TARGET's own
// catalog, so the card and the executor both name a model that session can run.
//
// It reads the settings to get the provider, which is also where the
// capability refusal comes from: resolving "opus" against a codex session's
// catalog would answer "there is no model called opus, the ones available are
// gpt-5..." — true, and not the thing worth saying about a session whose CLI
// cannot change model at all.
func prepareSetModel(ctx context.Context, s *Service, p *Proposal) map[string]any {
	spoken := strings.TrimSpace(stringArg(p.Args, "model"))
	if spoken == "" {
		return refuse("no-model", "NOTHING WAS PROPOSED: no model. Say which family they asked for.")
	}
	settings, err := s.actions.SessionSettings(ctx, p.SessionID)
	if err != nil {
		s.log.Warn("assistant: session settings unavailable", "session", p.SessionID, "error", err)
		return refuse("settings-unavailable", "NOTHING WAS PROPOSED: I could not read what that session "+
			"is running, so there is nothing to compare a new model against. Say so plainly.")
	}
	if refusal := modelSwitchRefusal(settings); refusal != "" {
		return refuse("model-switch-unsupported", fmt.Sprintf("NOTHING WAS PROPOSED, because %s. Say "+
			"that plainly as the answer -- it is the useful thing to know, not a failure.", refusal))
	}
	// A session on a paired machine resolves against THAT machine's catalog,
	// which is the one that decides what it can run.
	var choice ModelChoice
	if routed, ok := s.actions.(SessionModelResolver); ok {
		choice, err = routed.ResolveModelFor(ctx, p.SessionID, settings.Provider, spoken)
	} else {
		choice, err = s.actions.ResolveModel(ctx, settings.Provider, spoken)
	}
	if err != nil {
		var unknown *UnknownModelError
		if errors.As(err, &unknown) {
			return refuse("unknown-model", "NOTHING WAS PROPOSED: "+unknown.Error())
		}
		s.log.Warn("assistant: model not resolved", "spoken", spoken, "error", err)
		return refuse("model-unresolvable", "NOTHING WAS PROPOSED: I could not tell which model that "+
			"is. Say so plainly and offer to let them pick it on screen.")
	}
	p.Args["model"] = choice.ID
	p.Args["model_label"] = choice.Label
	return nil
}

// modelSwitchRefusal is why this session's model cannot be changed, or "".
//
// One sentence in one place because two readers need it: [prepareSetModel],
// which must not resolve a spoken name against a catalog the target cannot run
// anything from, and [checkSetModel], which is what re-reads the facts at
// accept time.
func modelSwitchRefusal(settings SessionSettings) string {
	if settings.ModelSwitch {
		return ""
	}
	return fmt.Sprintf("it runs %s, whose CLI cannot be moved to another model while it is running -- "+
		"which model it runs is settled when a turn starts it", providerWords(settings.Provider))
}

// providerWords names a session's CLI for a refusal, or says there is one
// without naming it. Never branched on: the capability bits decide, and this is
// only what the sentence calls them.
func providerWords(provider string) string {
	if provider == "" {
		return "a CLI"
	}
	return provider
}

func checkSetModel(ctx context.Context, s *Service, p Proposal) (map[string]any, string, error) {
	settings, err := s.actions.SessionSettings(ctx, p.SessionID)
	if err != nil {
		return nil, "", fmt.Errorf("session settings for %s: %w", p.SessionID, err)
	}
	evidence := settingsEvidence(settings, "model", settings.Model)
	wanted := stringArg(p.Args, "model")
	switch {
	case !settings.ModelSwitch:
		// Before the live check, because it is the more permanent fact: a codex
		// session cannot be switched whether or not its process is up.
		return evidence, modelSwitchRefusal(settings), nil
	case !settings.Live:
		return evidence, "its CLI is not running, so there is nothing to change the model on -- the " +
			"model is chosen when the next turn starts it", nil
	case wanted != "" && wanted == settings.Model:
		return evidence, "it is already running that model", nil
	}
	return evidence, "", nil
}

func execSetModel(ctx context.Context, s *Service, p Proposal) (string, error) {
	model := stringArg(p.Args, "model")
	if model == "" {
		// prepareSetModel always writes it, so this is the args column not
		// round-tripping. Nothing is performed on a blank: the setter would
		// hand the runtime an empty model name.
		return "", &OutcomeError{
			Outcome: "the model it was proposed with is not readable any more, so nothing was changed",
			Detail:  "empty model in the proposal's args",
		}
	}
	if err := s.actions.SetModel(ctx, p.SessionID, model); err != nil {
		return "", err
	}
	label := stringArg(p.Args, "model_label")
	if label == "" {
		label = model
	}
	return "now running " + label, nil
}

// settingsEvidence is what a set_* card quotes: the value being changed, the
// live flag, and the provider that decides whether the change is possible at
// all.
func settingsEvidence(settings SessionSettings, key, value string) map[string]any {
	evidence := map[string]any{key: value, "live": settings.Live}
	if settings.Provider != "" {
		evidence["provider"] = settings.Provider
	}
	return evidence
}

// prepareSetMode validates the mode against the closed set.
//
// It has to: the setter underneath silently coerces an unknown mode to
// "default", so a proposal that did not validate would be accepted, change the
// session to something nobody asked for, and report success.
func prepareSetMode(_ context.Context, _ *Service, p *Proposal) map[string]any {
	mode := strings.TrimSpace(stringArg(p.Args, "mode"))
	if !isPermissionMode(mode) {
		return refuse("bad-permission-mode", fmt.Sprintf("NOTHING WAS PROPOSED: %q is not a permission "+
			"mode. They are %s. Ask which they mean rather than picking one.",
			mode, SpokenList(permissionModes())))
	}
	p.Args["mode"] = mode
	return nil
}

// isPermissionMode reports whether a stored or spoken mode is one of the three.
func isPermissionMode(mode string) bool {
	for _, known := range permissionModes() {
		if mode == known {
			return true
		}
	}
	return false
}

// modeAvailable reports whether the target's provider HAS the permission mode
// being asked for.
//
// Codex implements neither plan nor acceptEdits, so it has no permission mode
// to be put in at all — `default` included, because "default" only means
// something where there is another mode to come back from.
func modeAvailable(settings SessionSettings, mode string) bool {
	switch mode {
	case PermissionModePlan:
		return settings.PlanMode
	case PermissionModeAcceptEdits:
		return settings.AcceptEditsMode
	default:
		return settings.PlanMode || settings.AcceptEditsMode
	}
}

func checkSetMode(ctx context.Context, s *Service, p Proposal) (map[string]any, string, error) {
	settings, err := s.actions.SessionSettings(ctx, p.SessionID)
	if err != nil {
		return nil, "", fmt.Errorf("session settings for %s: %w", p.SessionID, err)
	}
	evidence := settingsEvidence(settings, "mode", settings.Mode)
	wanted := stringArg(p.Args, "mode")
	switch {
	case !modeAvailable(settings, wanted):
		// Before the live check, for the same reason set_session_model's
		// capability refusal is: whether the CLI has permission modes at all
		// does not depend on whether its process is up.
		return evidence, fmt.Sprintf("it runs %s, which has no permission modes to be put in -- it "+
			"asks about each tool call instead", providerWords(settings.Provider)), nil
	case !settings.Live:
		return evidence, "its CLI is not running, so there is no permission mode to change -- the " +
			"mode it starts in is chosen when the next turn starts it", nil
	case wanted != "" && wanted == settings.Mode:
		return evidence, "it is already in that mode", nil
	}
	return evidence, "", nil
}

func execSetMode(ctx context.Context, s *Service, p Proposal) (string, error) {
	mode := stringArg(p.Args, "mode")
	if !isPermissionMode(mode) {
		// prepareSetMode validated it against the closed set, so this is the
		// args column not round-tripping — and the setter underneath COERCES an
		// unknown mode to "default" rather than refusing it, which would change
		// the session to something nobody asked for and report success.
		return "", &OutcomeError{
			Outcome: "the mode it was proposed with is not readable any more, so nothing was changed",
			Detail:  fmt.Sprintf("stored mode %q is not one of %v", mode, permissionModes()),
		}
	}
	if err := s.actions.SetMode(ctx, p.SessionID, mode); err != nil {
		return "", err
	}
	return "permission mode is now " + mode, nil
}

// commits renders a commit count the way a sentence reads it.
func commits(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

// encodeJSONObject renders a JSON column, degrading to an empty object rather
// than costing the row: what was asked for still happened, and the summary is
// the part that is read.
func encodeJSONObject(s *Service, what string, obj map[string]any) string {
	if len(obj) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		s.log.Warn("assistant: json column dropped", "what", what, "error", err)
		return "{}"
	}
	return string(encoded)
}

// decodeJSONObject reads one back. An unreadable column is nothing rather than
// an error: a card with no evidence on it is still a card.
func decodeJSONObject(raw string) map[string]any {
	if raw == "" || raw == "{}" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
