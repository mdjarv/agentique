// Package mcphttp wires agentique's tools (channel SendMessage, dev URL
// management, session renaming) onto agentkit's reusable MCP-over-HTTP
// handler. The transport, schema, and dispatch live in agentkit/mcphttp;
// this package only contributes the agentique-specific tool implementations
// and tool-name constants that the permission interceptor keys on.
package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/allbin/agentkit/devurls"
	akmcp "github.com/allbin/agentkit/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/procctl"
)

// Tool name constants. The full names (mcp__<server>__<tool>) drive the
// permission interceptor in the session package, so they live here as the
// product-internal source of truth even though agentkit's mcphttp package
// is what actually serves them.
const (
	ServerName          = "agentique"
	ToolSendMessage     = "SendMessage"
	ToolAcquireDev      = "AcquireDevUrl"
	ToolReleaseDev      = "ReleaseDevUrl"
	ToolListDevURLs     = "ListDevUrls"
	ToolKillDevPort     = "KillDevUrlPort"
	ToolSetSessionName  = "SetSessionName"
	ToolSuggestPrompt   = "SuggestSessionPrompt"
	ToolScheduleCreate  = "ScheduleCreate"
	ToolScheduleReport  = "ScheduleReport"
	ToolScheduleNext    = "ScheduleNext"
	ToolAssistantReport = "AssistantReport"
	ToolSessionModel    = "SessionModel"

	// AssistantReportToolFullName is the name the dispatched prompt tells a
	// worker to call while the assistant is following the run.
	//
	// It renamed from VoiceReport with no wire transition, and needs none: the
	// tool is in-process, and its name reaches a session only through the
	// instruction that teaches it, which is written from this constant.
	AssistantReportToolFullName = "mcp__" + ServerName + "__" + ToolAssistantReport

	SendMessageToolFullName    = "mcp__" + ServerName + "__" + ToolSendMessage
	AcquireDevURLToolFullName  = "mcp__" + ServerName + "__" + ToolAcquireDev
	ReleaseDevURLToolFullName  = "mcp__" + ServerName + "__" + ToolReleaseDev
	ListDevURLsToolFullName    = "mcp__" + ServerName + "__" + ToolListDevURLs
	SetSessionNameToolFullName = "mcp__" + ServerName + "__" + ToolSetSessionName
	// SessionModelToolFullName is read-only, so it auto-approves like
	// SetSessionName.
	SessionModelToolFullName = "mcp__" + ServerName + "__" + ToolSessionModel
	// KillDevPortToolFullName is NOT auto-approved: killing a process is
	// destructive, so the user must confirm each invocation.
	KillDevPortToolFullName = "mcp__" + ServerName + "__" + ToolKillDevPort
)

// serverVersion is reported in the initialize response. Bumped when the
// tool surface changes in a user-visible way.
const serverVersion = "1.0.0"

// maxSessionName clamps the SetSessionName argument so an over-eager agent
// can't write a paragraph into the sidebar.
const maxSessionName = 80

// TokenStore is the per-session bearer-token store backing the /mcp endpoint.
// Aliased from agentkit so call sites in this repo don't need to import the
// upstream package directly.
type TokenStore = akmcp.TokenStore

// NewTokenStore returns an empty TokenStore.
func NewTokenStore() *TokenStore { return akmcp.NewTokenStore() }

// SessionRenamer renames an existing session. Implemented by session.Service.
type SessionRenamer interface {
	RenameSession(ctx context.Context, sessionID, name string) error
}

// ScheduleCreator is the scheduled-loops tool surface: propose a schedule
// (paused, pending approval) and report a run's outcome. Implemented by
// schedule.Scheduler. May be nil — the schedule tools are then not registered.
type ScheduleCreator interface {
	AgentCreate(ctx context.Context, sessionID, name, prompt, cron, at string, dynamic bool) (string, error)
	AgentReport(ctx context.Context, sessionID, runID, status, summary string) (string, error)
	AgentPace(ctx context.Context, sessionID, runID string, delaySeconds int, reason string, stop bool) (string, error)
}

// The assistant's tool surface has two callers that must never be able to reach
// each other's half, and the two halves appear and disappear independently — so
// they are two interfaces, each nil when its caller does not exist, on two
// ENDPOINTS.
//
// A coding SESSION gets [ToolAssistantReport] and nothing else: a worker
// telling the operator something only it could know. The assistant's own HEAD
// gets the verb table and nothing else: the closed set of things the assistant
// is allowed to do, with every refusal, tier gate and rate limit enforced
// inside it rather than in either prompt.
//
// Two endpoints rather than one, because `tools/list` is not scoped to the
// caller: a handler answers it from everything registered on it, whoever asks.
// One shared endpoint therefore handed twenty verbs — merge_session,
// delete_session and the rest of the uncontained tier included, which exist in
// the table only to be refused — to every coding session on every turn. That is
// context those sessions pay for, tool names they can never call, and, for a
// prompt-injected one, the assistant's own vocabulary to aim a crafted report
// at. [NewAssistantHandler] is the head's, mounted for head tokens only;
// [registerHeadTool] stays as the second belt, since the id test is what makes
// the separation total rather than a matter of which URL a config named.

// AssistantReporter relays one worker report to whoever is following the run.
//
// It exists whenever a report has somewhere to go, which is NOT the same as the
// assistant being switched on: a live call follows runs through the report
// registry alone, and the instruction that teaches a worker this tool rides
// every prompt the dispatcher sends on a call. Gating the tool on the assistant
// service would name a tool that is not registered in every voice dispatch on a
// server whose assistant is off.
//
// Implemented by *assistant.Service (journal and followers) and by
// *assistant.Registry (followers only). May be nil — [ToolAssistantReport] is
// then not registered.
type AssistantReporter interface {
	// Report relays one worker report. Nobody following is a normal answer, not
	// an error: the message says so, and the worker can stop calling.
	Report(sessionID, kind, headline string) (string, error)
}

// AssistantHead is the verb table, and it exists exactly when the assistant
// does: the head is what calls a verb, and every rule that matters lives in
// [AssistantHead.ToolHandler] rather than in a prompt.
//
// Implemented by *assistant.Service. It is served by [NewAssistantHandler] on
// the head's own endpoint, so a coding session's tool list never grows by the
// table.
type AssistantHead interface {
	// Verbs is the closed table, in enough detail to build a tool schema from.
	Verbs() []assistant.Verb
	// ToolHandler runs one verb and always answers — the caller is a model that
	// stays paused until it is, so a refusal is a payload and never an error.
	ToolHandler(ctx context.Context, name string, args map[string]any) map[string]any
}

// headOnlyRefusal is what a coding session is told when it calls a verb that
// belongs to the assistant's head.
//
// Written for a model to read and stop: the verbs are not a capability a session
// has been given badly, they are somebody else's tools sharing one endpoint.
const headOnlyRefusal = "That tool belongs to the assistant, not to a session. It is not yours to " +
	"call; ask the operator to do it from the assistant instead."

// sessionOnlyRefusal is the mirror: the assistant's head calling a tool that
// acts on the session whose identity it would be borrowing.
const sessionOnlyRefusal = "That tool acts on a coding session, and you are not one. Use your own " +
	"tools instead."

// SessionModelReport is the JSON the SessionModel tool answers with: which
// upstream model a session is actually running on, and how strong that reading
// is. Field-identical to session.ModelReport, kept separate because the session
// package imports this one (Manager.SetMCPHTTP) and the dependency cannot run
// both ways — the wiring site adapts.
type SessionModelReport struct {
	SessionID       string `json:"sessionId"`
	Provider        string `json:"provider"`
	RequestedSlug   string `json:"requestedSlug"`
	ResolvedModelID string `json:"resolvedModelId,omitempty"`
	ResolvedAt      string `json:"resolvedAt,omitempty"`
	Source          string `json:"source"`
}

// SessionModelInspector answers which upstream model a session runs on.
// Implemented by an adapter over session.Service. May be nil — SessionModel is
// then not registered.
type SessionModelInspector interface {
	InspectSessionModel(ctx context.Context, sessionID string) (SessionModelReport, error)
}

// NewHandler returns the configured /mcp http.Handler — the endpoint every
// coding session reaches. renamer may be nil in tests that don't exercise
// SetSessionName — calls to that tool will then return an error result. sched
// may be nil to omit ScheduleCreate; reporter may be nil to omit
// AssistantReport; models may be nil to omit SessionModel.
//
// A coding session reaches no memory tool. The brain is the assistant's
// long-term memory and sessions never write facts (docs/assistant.md, the M2
// contract); what a session learns travels as an [ToolAssistantReport], which is
// untrusted text in the journal.
//
// The assistant's verb table is deliberately NOT here: it is the head's, on the
// head's own endpoint ([NewAssistantHandler]), because a tool list is not scoped
// to the caller.
func NewHandler(tokens *TokenStore, dev *devurls.Store, renamer SessionRenamer, sched ScheduleCreator, reporter AssistantReporter, models SessionModelInspector) http.Handler {
	h := akmcp.New(ServerName, tokens, akmcp.WithServerVersion(serverVersion))

	registerSessionTool(h, akmcp.Tool{
		Name:        ToolSendMessage,
		Description: "Send a message to a teammate in this channel.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"to": akmcp.StringProp{
					Description: "Recipient: teammate name, \"@spawn\" to create workers, \"@release\" to file idle workers away (reversible — keeps their branches), or \"@dissolve\" to close the channel and delete its workers.",
				},
				"message": akmcp.StringProp{
					Description: "Message content. For @spawn, a JSON string with channelName and workers array.",
				},
				"type": akmcp.StringProp{
					Enum:        []string{"plan", "progress", "done", "message"},
					Description: "Message type for status signaling.",
				},
			},
			Required: []string{"to", "message"},
		},
		Handler: func(_ context.Context, _ string, _ json.RawMessage) akmcp.Result {
			// Should never reach here — Claude's permission gate intercepts
			// SendMessage before it executes. Return a benign success to match
			// mcp-channel behavior.
			return akmcp.TextResult("Message delivered.")
		},
	})

	registerSessionTool(h, akmcp.Tool{
		Name:        ToolAcquireDev,
		Description: "Lease a publicly-routable HTTPS URL that points at a local TCP port on this machine. Bind any HTTP service to the returned port and it becomes reachable at the returned URL (TLS terminated by the reverse proxy — valid certificate, so HTTPS-only features like passkeys/WebAuthn, secure cookies, and service workers work). Returns {slot, url, publicHost, port}. Idempotent — re-calling returns the existing lease for this session.",
		Handler: func(ctx context.Context, sid string, _ json.RawMessage) akmcp.Result {
			return acquireDevImpl(ctx, dev, sid)
		},
	})

	registerSessionTool(h, akmcp.Tool{
		Name:        ToolReleaseDev,
		Description: "Release any dev URL slot leased by this session. Idempotent — no-op if nothing is held. Slots also auto-release when the session ends.",
		Handler: func(_ context.Context, sid string, _ json.RawMessage) akmcp.Result {
			return releaseDevImpl(dev, sid)
		},
	})

	registerSessionTool(h, akmcp.Tool{
		Name:        ToolListDevURLs,
		Description: "List all configured dev URL slots, their current holders, and whether each port is actually bound. Includes external-owner details (pid, cmdline, cwd) when a port is bound by a process not tracked by the lease store — useful for spotting orphans that need KillDevUrlPort.",
		Handler: func(ctx context.Context, _ string, _ json.RawMessage) akmcp.Result {
			return listDevURLsImpl(ctx, dev)
		},
	})

	type setNameArgs struct {
		Name string `json:"name"`
	}
	registerSessionTool(h, akmcp.Tool{
		Name:        ToolSetSessionName,
		Description: "Rename the current Agentique session. Use when the session's topic becomes clear or the user asks for a rename. Keep the name short (a few words, max 80 chars) and descriptive of what the session is about — it appears in the sidebar. The UI updates immediately.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"name": akmcp.StringProp{
					Description: "New session title. Short, human-readable, no trailing punctuation.",
				},
			},
			Required: []string{"name"},
		},
		Handler: akmcp.TypedHandler(func(ctx context.Context, sid string, args setNameArgs) akmcp.Result {
			return setSessionNameImpl(ctx, renamer, sid, args.Name)
		}),
	})

	type killSlotArgs struct {
		Slot string `json:"slot"`
	}
	registerSessionTool(h, akmcp.Tool{
		Name:        ToolKillDevPort,
		Description: "Terminate the process currently listening on a dev URL slot's TCP port. Use when AcquireDevUrl skipped a slot or ListDevUrls reports an external/orphan owner. SIGTERM → 2s wait → SIGKILL. Destructive — requires user confirmation each call. After success, retry AcquireDevUrl.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"slot": akmcp.StringProp{
					Description: "Slot name (e.g. \"dev1\"). See ListDevUrls for configured slots.",
				},
			},
			Required: []string{"slot"},
		},
		Handler: akmcp.TypedHandler(func(ctx context.Context, _ string, args killSlotArgs) akmcp.Result {
			return killDevPortImpl(ctx, dev, args.Slot)
		}),
	})

	type suggestPromptArgs struct {
		Title   string `json:"title"`
		Prompt  string `json:"prompt"`
		Project string `json:"project"`
	}
	registerSessionTool(h, akmcp.Tool{
		Name: ToolSuggestPrompt,
		Description: "Surface a ready-to-launch session prompt to the user as a clickable card. " +
			"Use when you spot independent work that could run as its own parallel session — call " +
			"once per suggestion. `title` becomes the new session's name; `prompt` is the full, " +
			"self-contained task for it (the new session sees only this prompt, NOT the current " +
			"conversation, so include the paths, conventions, and interfaces it needs). Pass " +
			"`project` (a project slug) to target a different project; omit it for the current one. " +
			"Only suggest genuinely parallelizable work.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"title":   akmcp.StringProp{Description: "Short session name (a few words, max ~80 chars)."},
				"prompt":  akmcp.StringProp{Description: "Full, self-contained task for the new session."},
				"project": akmcp.StringProp{Description: "Optional project slug to target a different project."},
			},
			Required: []string{"title", "prompt"},
		},
		// Pure UI affordance — the card renders from the persisted tool_use event on
		// the frontend, so there is no server-side side effect to perform here. The
		// call is auto-allowed by the session's tool interceptor; this handler just
		// acks the model. Unlike SendMessage it needs no deny-and-route dance.
		Handler: akmcp.TypedHandler(func(_ context.Context, _ string, _ suggestPromptArgs) akmcp.Result {
			return akmcp.TextResult("Suggestion surfaced to the user as a launchable card.")
		}),
	})

	type sessionModelArgs struct {
		SessionID string `json:"sessionId"`
	}
	if models != nil {
		registerSessionTool(h, akmcp.Tool{
			Name: ToolSessionModel,
			Description: "Report which upstream model an Agentique session is actually running on. " +
				"The requested model is an alias (\"opus\", \"sonnet\") that moves between releases, so it " +
				"does not name the model that answered — this does. Omit `sessionId` for the session you " +
				"are running in; pass one to inspect another session. Answers JSON: sessionId, provider, " +
				"requestedSlug, resolvedModelId, resolvedAt, and source — `init_event` (that session's own " +
				"run reported the id), `catalog` (the session never reported one, so this is what the same " +
				"slug resolved to elsewhere — a hint, not this session's history), or `unresolved` (nothing " +
				"has reported a model for this slug yet). Read-only.",
			InputSchema: akmcp.ObjectProp{
				Properties: map[string]akmcp.Property{
					"sessionId": akmcp.StringProp{
						Description: "Session to inspect. Omit for the calling session.",
					},
				},
			},
			Handler: akmcp.TypedHandler(func(ctx context.Context, sid string, args sessionModelArgs) akmcp.Result {
				return sessionModelImpl(ctx, models, sid, args.SessionID)
			}),
		})
	}

	if sched != nil {
		registerScheduleTools(h, sched)
	}
	if reporter != nil {
		registerAssistantReportTool(h, reporter)
	}

	return h
}

// NewAssistantHandler returns the http.Handler for the assistant head's own MCP
// endpoint: the verb table, and nothing else on it.
//
// Its own TokenStore as well as its own path, so the only caller that can
// authenticate to it is one the head manager minted a token for. A coding
// session's bearer is not in this store, which is what makes "a session never
// sees the verb table" true of the LISTING and not only of the calls.
func NewAssistantHandler(tokens *TokenStore, head AssistantHead) http.Handler {
	h := akmcp.New(ServerName, tokens, akmcp.WithServerVersion(serverVersion))
	if head == nil {
		return h
	}
	// Every verb is registered, uncontained ones included: the table is what the
	// head is allowed to KNOW about as well as what it may do, and a verb it
	// cannot see is one it invents a way around.
	for _, verb := range head.Verbs() {
		registerVerbTool(h, head, verb)
	}
	return h
}

// registerAssistantReportTool exposes the worker's half of the assistant's tool
// surface: what a session tells the assistant.
//
// The direction matters: the worker pushes what IT decides is worth saying,
// rather than a watcher inferring salience from its event stream. Only the
// worker knows it just found the tests were already broken, so the judgement
// lives where the knowledge is — and the inference layer that would otherwise be
// needed does not exist.
//
// The verbs run the other way and are the head's. Every rule that matters is
// inside them ([AssistantHead.ToolHandler]) rather than in the head's prompt,
// which is what makes two heads on one body safe: a call and the thread hit the
// same refusals.
func registerAssistantReportTool(h *akmcp.Handler, a AssistantReporter) {
	type assistantReportArgs struct {
		Kind     string `json:"kind"`
		Headline string `json:"headline"`
	}
	registerSessionTool(h, akmcp.Tool{
		Name: ToolAssistantReport,
		Description: "Tell the operator's assistant something about this run, so it reaches whoever is " +
			"following — read aloud on a live call, and kept in the assistant's journal either way. " +
			"Call it at decision points and surprises — the things that would change what they'd ask " +
			"you to do next (a test suite that was already failing, a file that isn't where the task " +
			"assumed, an approach you've abandoned). Do NOT report progress: opening files, running " +
			"commands and finishing routine steps are all visible on their screen. Expect two or three " +
			"calls in a ten-minute run, not twenty — reporting too often trains them to stop listening. " +
			"You do not need to report finishing; that is delivered automatically. The headline may be " +
			"READ ALOUD, so write one plain spoken sentence: no markdown, no bullets, no code. If nobody " +
			"is following, it is kept rather than spoken, and the answer says so.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"kind": akmcp.StringProp{
					Enum: []string{"surprise", "decision", "milestone"},
					Description: "surprise: something contradicts the task's premise. decision: a fork you " +
						"took that they might have taken differently. milestone: a meaningful step finished " +
						"in a long run.",
				},
				"headline": akmcp.StringProp{
					Description: "One plain spoken sentence, written to the person: \"the auth tests were " +
						"already failing on main\", not \"Note: pre-existing test failures detected.\"",
				},
			},
			Required: []string{"kind", "headline"},
		},
		Handler: akmcp.TypedHandler(func(_ context.Context, sid string, args assistantReportArgs) akmcp.Result {
			msg, err := a.Report(sid, args.Kind, args.Headline)
			if err != nil {
				return akmcp.ErrorResultf("assistant report failed: %v", err)
			}
			return akmcp.TextResult(msg)
		}),
	})
}

// registerVerbTool exposes one verb from the closed table to the head.
//
// Asking for an uncontained one answers a refusal naming its tier, which until
// proposals exist (M3) is the whole of the answer.
func registerVerbTool(h *akmcp.Handler, a AssistantHead, verb assistant.Verb) {
	registerHeadTool(h, akmcp.Tool{
		Name:        verb.Name,
		Description: verb.Description,
		InputSchema: verbSchema(verb),
		Handler: func(ctx context.Context, _ string, raw json.RawMessage) akmcp.Result {
			args := map[string]any{}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					// Not an error result: the model is paused until it is
					// answered, and "your arguments did not parse" is something it
					// can act on where a transport failure is not.
					return akmcp.TextResult("Those arguments did not parse as JSON. Say plainly that it " +
						"did not go through, and try once more.")
				}
			}
			return verbResult(a.ToolHandler(ctx, verb.Name, args))
		},
	})
}

// verbSchema turns one verb's parameter list into a tool schema.
//
// The table describes its arguments in its own terms ([assistant.Param]) so that
// package never has to know what an MCP schema looks like; this is the one place
// the two vocabularies meet.
func verbSchema(verb assistant.Verb) akmcp.ObjectProp {
	schema := akmcp.ObjectProp{Properties: map[string]akmcp.Property{}}
	for _, param := range verb.Input {
		switch param.Type {
		case assistant.ParamBoolean:
			schema.Properties[param.Name] = akmcp.BoolProp{Description: param.Description}
		case assistant.ParamInteger:
			schema.Properties[param.Name] = akmcp.NumberProp{Description: param.Description}
		default:
			schema.Properties[param.Name] = akmcp.StringProp{
				Description: param.Description,
				Enum:        param.Enum,
			}
		}
		if param.Required {
			schema.Required = append(schema.Required, param.Name)
		}
	}
	return schema
}

// verbResult renders a verb's payload for the head.
//
// A refusal comes back as an `error` key rather than as a Go error, because the
// caller is a model paused until it is answered and an unanswered tool call is
// indistinguishable from the whole thing having died. It is still marked as an
// error result, so the head reads it as a refusal rather than as data.
func verbResult(payload map[string]any) akmcp.Result {
	if payload == nil {
		return akmcp.TextResult("{}")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return akmcp.ErrorResultf("encode verb result: %v", err)
	}
	if _, refused := payload["error"].(string); refused {
		return akmcp.ErrorResult(string(body))
	}
	return akmcp.TextResult(string(body))
}

func registerScheduleTools(h *akmcp.Handler, sched ScheduleCreator) {
	type createArgs struct {
		Name    string `json:"name"`
		Prompt  string `json:"prompt"`
		Cron    string `json:"cron"`
		At      string `json:"at"`
		Dynamic bool   `json:"dynamic"`
	}
	registerSessionTool(h, akmcp.Tool{
		Name:        ToolScheduleCreate,
		Description: "Propose a scheduled loop on THIS session: agentique will re-send the prompt as a fresh turn on the schedule, durably (survives restarts and idle eviction). The schedule is created PAUSED, awaiting the user's approval in the UI — it never fires before approval, and this call returns immediately (do not wait for approval; tell the user and move on). Provide exactly one of `cron` (recurring), `at` (one-shot reminder), or `dynamic: true` (self-paced: first fire immediate, then the agent picks each delay via ScheduleNext).",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"name":    akmcp.StringProp{Description: "Short human-readable name for the loop (shown in the schedules UI)."},
				"prompt":  akmcp.StringProp{Description: "The prompt to run each fire. Self-contained: it is re-sent verbatim every time."},
				"cron":    akmcp.StringProp{Description: "5-field cron expression in the server's local timezone (e.g. \"*/30 * * * *\", \"0 9 * * 1-5\"). Supported: wildcards, values, steps, ranges, lists. No names/L/W."},
				"at":      akmcp.StringProp{Description: "RFC3339 time for a one-shot reminder (e.g. \"2026-07-31T15:00:00+02:00\"). Mutually exclusive with cron/dynamic."},
				"dynamic": akmcp.BoolProp{Description: "Self-paced loop: the running agent chooses each next delay with ScheduleNext; a forgotten reschedule falls back to a fixed delay, and a loop that stops rescheduling parks visibly."},
			},
			Required: []string{"name", "prompt"},
		},
		Handler: akmcp.TypedHandler(func(ctx context.Context, sid string, args createArgs) akmcp.Result {
			msg, err := sched.AgentCreate(ctx, sid, args.Name, args.Prompt, args.Cron, args.At, args.Dynamic)
			if err != nil {
				return akmcp.ErrorResultf("schedule create failed: %v", err)
			}
			return akmcp.TextResult(msg)
		}),
	})

	type reportArgs struct {
		RunID   string `json:"runId"`
		Status  string `json:"status"`
		Summary string `json:"summary"`
	}
	registerSessionTool(h, akmcp.Tool{
		Name:        ToolScheduleReport,
		Description: "Report the outcome of the scheduled run you are currently executing (the runId is in the [scheduled-run:…] footer of the prompt that started this turn). Call it once, when the run's work is done: status `ok` (worked), `action-needed` (a human must look — this raises attention without failing the loop), or `failed` (the run genuinely failed). The summary becomes the run's one-line history entry. Only valid for runs fired into THIS session.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"runId": akmcp.StringProp{Description: "The run id from the [scheduled-run:…] footer of this turn's prompt."},
				"status": akmcp.StringProp{
					Enum:        []string{"ok", "action-needed", "failed"},
					Description: "Outcome of this run.",
				},
				"summary": akmcp.StringProp{Description: "One line: what happened / what needs the human."},
			},
			Required: []string{"runId", "status", "summary"},
		},
		Handler: akmcp.TypedHandler(func(ctx context.Context, sid string, args reportArgs) akmcp.Result {
			msg, err := sched.AgentReport(ctx, sid, args.RunID, args.Status, args.Summary)
			if err != nil {
				return akmcp.ErrorResultf("schedule report failed: %v", err)
			}
			return akmcp.TextResult(msg)
		}),
	})

	type paceArgs struct {
		RunID        string `json:"runId"`
		DelaySeconds int    `json:"delaySeconds"`
		Reason       string `json:"reason"`
		Stop         bool   `json:"stop"`
	}
	registerSessionTool(h, akmcp.Tool{
		Name:        ToolScheduleNext,
		Description: "Self-paced loops only: choose when this loop should fire again (the runId is in the [scheduled-run:…] footer of this turn's prompt). Pass delaySeconds + a short reason shown to the user (\"waiting for CI run to finish\"), or stop=true when the loop's goal is complete — the loop then parks visibly and the user can resume it later. Delays are clamped to the server's configured bounds. Without a call, one fallback fire happens; a loop that stops rescheduling is parked.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"runId":        akmcp.StringProp{Description: "The run id from the [scheduled-run:…] footer of this turn's prompt."},
				"delaySeconds": akmcp.NumberProp{Description: "Seconds until the next fire (ignored when stop=true)."},
				"reason":       akmcp.StringProp{Description: "Short human-readable reason for the chosen delay, rendered in the UI."},
				"stop":         akmcp.BoolProp{Description: "End the loop: park the schedule instead of scheduling another fire."},
			},
			Required: []string{"runId"},
		},
		Handler: akmcp.TypedHandler(func(ctx context.Context, sid string, args paceArgs) akmcp.Result {
			msg, err := sched.AgentPace(ctx, sid, args.RunID, args.DelaySeconds, args.Reason, args.Stop)
			if err != nil {
				return akmcp.ErrorResultf("schedule next failed: %v", err)
			}
			return akmcp.TextResult(msg)
		}),
	})
}

// register panics on registration failures because they indicate programmer
// errors (duplicate names, malformed schemas) that must be caught at startup,
// not at request time.
func register(h *akmcp.Handler, t akmcp.Tool) {
	if err := h.Register(t); err != nil {
		panic(fmt.Sprintf("mcphttp: register %q: %v", t.Name, err))
	}
}

// registerSessionTool is [register] for a tool that only a coding session may
// call.
//
// One endpoint serves two kinds of caller now: every session, and the
// assistant's own head. They are told apart by the injected id and nothing else
// — a session's is a UUID, the head's carries [assistant.HeadIDPrefix] — so the
// test lives here, at the one place a tool is registered, rather than at the top
// of each handler where the next tool would forget it. A tool that acts on "the
// calling session" must refuse a caller that is not one: the head would
// otherwise be renaming, or sending as, a session it has merely borrowed the
// identity of.
func registerSessionTool(h *akmcp.Handler, t akmcp.Tool) {
	inner := t.Handler
	t.Handler = func(ctx context.Context, sid string, args json.RawMessage) akmcp.Result {
		if assistant.IsHeadID(sid) {
			return akmcp.ErrorResult(sessionOnlyRefusal)
		}
		return inner(ctx, sid, args)
	}
	register(h, t)
}

// registerHeadTool is the mirror: a tool only the assistant's head may call.
func registerHeadTool(h *akmcp.Handler, t akmcp.Tool) {
	inner := t.Handler
	t.Handler = func(ctx context.Context, sid string, args json.RawMessage) akmcp.Result {
		if !assistant.IsHeadID(sid) {
			return akmcp.ErrorResult(headOnlyRefusal)
		}
		return inner(ctx, sid, args)
	}
	register(h, t)
}

// --- tool implementations ---

func acquireDevImpl(ctx context.Context, dev *devurls.Store, sessionID string) akmcp.Result {
	if len(dev.Slots(ctx)) == 0 {
		return akmcp.ErrorResult("No dev URL slots are configured on this server. Ask the operator to add [[dev-urls]] entries to agentique config.")
	}
	res, err := dev.Acquire(ctx, sessionID)
	if err != nil {
		if errors.Is(err, devurls.ErrAllBusy) {
			return akmcp.ErrorResult("All dev URL slots are currently in use.\n" + summarizeSlotState(dev.Slots(ctx)) + "\n\n" +
				"Use KillDevUrlPort with a specific slot name to reclaim a port held by an external/orphan process (requires user confirmation).")
		}
		return akmcp.ErrorResultf("acquire failed: %v", err)
	}
	lease := res.Lease
	msg := fmt.Sprintf(
		"Acquired dev URL slot %q.\n"+
			"Public URL: %s (TLS-terminated by the reverse proxy)\n"+
			"Local port: %d\n"+
			"Public host: %s\n\n"+
			"Bind any HTTP service to 127.0.0.1:%d (or 0.0.0.0:%d) and it becomes reachable at the URL. Examples:\n"+
			"  - Vite dev server:  `just dev-frontend-remote %d %s` (Agentique) or `vite --port %d --host`\n"+
			"  - Go HTTP server:   pass `--addr :%d` or `http.ListenAndServe(\":%d\", ...)`\n"+
			"  - Any bind-to-port process works (static file servers, tunneled demos, etc.)\n\n"+
			"Release with ReleaseDevUrl when done (auto-released at session end).",
		lease.Slot, lease.URL, lease.Port, lease.PublicHost,
		lease.Port, lease.Port,
		lease.Port, lease.PublicHost, lease.Port,
		lease.Port, lease.Port,
	)
	if len(res.Skipped) > 0 {
		msg += "\n\nNote: skipped these slots because their ports are bound by external/orphan processes:\n" +
			formatConflicts(res.Skipped) +
			"\nConsider calling KillDevUrlPort to clean them up."
	}
	return akmcp.TextResult(msg)
}

func releaseDevImpl(dev *devurls.Store, sessionID string) akmcp.Result {
	freed := dev.Release(sessionID)
	if len(freed) == 0 {
		return akmcp.TextResult("No dev URL slot was held by this session.")
	}
	return akmcp.TextResult("Released slot(s): " + strings.Join(freed, ", "))
}

func listDevURLsImpl(ctx context.Context, dev *devurls.Store) akmcp.Result {
	infos := dev.Slots(ctx)
	if len(infos) == 0 {
		return akmcp.TextResult("No dev URL slots are configured.")
	}
	return akmcp.TextResult("Dev URL slots:\n" + summarizeSlotState(infos))
}

func killDevPortImpl(ctx context.Context, dev *devurls.Store, slotName string) akmcp.Result {
	if strings.TrimSpace(slotName) == "" {
		return akmcp.ErrorResult("KillDevUrlPort requires { slot: \"<slot name>\" }. Use ListDevUrls to see configured slots.")
	}
	slot, ok := dev.FindSlot(slotName)
	if !ok {
		return akmcp.ErrorResultf("unknown slot %q", slotName)
	}
	owner, err := devurls.FindPortOwner(ctx, slot.Port)
	if err != nil {
		return akmcp.ErrorResultf("lookup owner for port %d: %v", slot.Port, err)
	}
	if owner == nil {
		// Nothing bound. Also clear any stale lease so the slot is reusable.
		dev.ReleaseSlot(slot.Slot)
		return akmcp.TextResultf("Port %d is already free. Cleared any stale lease on slot %q.", slot.Port, slot.Slot)
	}

	_ = procctl.Terminate(owner.PID)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !procctl.Alive(owner.PID) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	killed := false
	if procctl.Alive(owner.PID) {
		_ = procctl.Kill(owner.PID)
		time.Sleep(200 * time.Millisecond)
		killed = procctl.Alive(owner.PID) // true means the kill also failed
	}

	// Clear any lease tracking this slot — whoever held it is gone now.
	dev.ReleaseSlot(slot.Slot)

	if killed {
		return akmcp.ErrorResultf("Sent SIGTERM+SIGKILL to pid %d but it is still alive. Manual intervention needed.", owner.PID)
	}
	return akmcp.TextResultf("Killed pid %d (%s). Slot %q (port %d) is free — retry AcquireDevUrl.",
		owner.PID, owner.Describe(), slot.Slot, slot.Port)
}

func setSessionNameImpl(ctx context.Context, renamer SessionRenamer, sessionID, raw string) akmcp.Result {
	if renamer == nil {
		return akmcp.ErrorResult("SetSessionName is not available in this server.")
	}
	name := strings.TrimSpace(raw)
	if name == "" {
		return akmcp.ErrorResult("name is empty — provide a short human-readable title.")
	}
	if len(name) > maxSessionName {
		name = strings.TrimSpace(name[:maxSessionName])
	}
	if err := renamer.RenameSession(ctx, sessionID, name); err != nil {
		return akmcp.ErrorResultf("rename failed: %v", err)
	}
	return akmcp.TextResultf("Session renamed to %q.", name)
}

// sessionModelImpl answers the SessionModel tool. The argument defaults to the
// calling session, which is the common case: an agent asking what it is.
func sessionModelImpl(ctx context.Context, models SessionModelInspector, callerID, argID string) akmcp.Result {
	target := strings.TrimSpace(argID)
	if target == "" {
		target = callerID
	}
	if target == "" {
		return akmcp.ErrorResult("no session to inspect: this call carries no session identity, so pass { sessionId: \"<id>\" }.")
	}

	report, err := models.InspectSessionModel(ctx, target)
	if err != nil {
		return akmcp.ErrorResultf("session model lookup failed: %v", err)
	}
	body, err := json.Marshal(report)
	if err != nil {
		return akmcp.ErrorResultf("encode session model report: %v", err)
	}
	return akmcp.TextResult(string(body))
}

func summarizeSlotState(infos []devurls.SlotInfo) string {
	sorted := make([]devurls.SlotInfo, len(infos))
	copy(sorted, infos)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Slot < sorted[j].Slot })
	lines := make([]string, 0, len(sorted))
	for _, i := range sorted {
		status := "(free)"
		switch {
		case i.HolderSessionID != "" && i.PortBusy:
			status = fmt.Sprintf("held by %s (port bound)", i.HolderSessionID)
		case i.HolderSessionID != "" && !i.PortBusy:
			status = fmt.Sprintf("leased by %s but port is NOT bound — stale lease", i.HolderSessionID)
		case i.HolderSessionID == "" && i.PortBusy:
			status = "external owner — " + i.ExternalOwner.Describe()
		}
		lines = append(lines, fmt.Sprintf("- %s → %s (port %d): %s", i.Slot, i.URL, i.Port, status))
	}
	return strings.Join(lines, "\n")
}

func formatConflicts(cs []devurls.SlotConflict) string {
	lines := make([]string, 0, len(cs))
	for _, c := range cs {
		lines = append(lines, fmt.Sprintf("- %s (port %d): %s", c.Slot, c.Port, c.Owner.Describe()))
	}
	return strings.Join(lines, "\n")
}
