package assistant

import (
	"context"
	"fmt"
	"regexp"
	"sync"
)

// Surface names, which are also the keys a surface's seen marks are stored
// under. They are a closed set in practice and an open one in the schema: a
// gateway adds one without a migration.
const (
	// SurfaceThread is the app's /assistant page: a transport, and the one
	// surface that can show a card.
	SurfaceThread = "thread"
	// SurfaceVoice is a live call: a head, and blind.
	SurfaceVoice = "voice"
	// SurfaceHeartbeat is not a surface either — it is where a turn the
	// heartbeat started was said. A message carries it so the thread can tell a
	// turn nobody asked for from one the operator typed, and so the head's own
	// news rendering does not read a heartbeat turn back to it as a
	// conversation somebody had.
	SurfaceHeartbeat = "heartbeat"
	// SurfaceHead is not a surface at all — it is the core's own head reading
	// the news it has not been told yet. It shares the seen-mark mechanism
	// because "what has happened since I last looked" is the same question.
	SurfaceHead = "head"
)

// surfaceNamePattern is what a surface name may be.
//
// It is not cosmetic: the name is interpolated into a JSON path
// (`json_set(seen_by, '$.' || ?, ?)`), so a name carrying a quote or a dot
// would address a different key or none at all. Validated at the door rather
// than escaped at each call site.
var surfaceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// checkSurface is the guard every surface-keyed call runs first.
func checkSurface(surface string) error {
	if !surfaceNamePattern.MatchString(surface) {
		return fmt.Errorf("%q is not a surface name (want %s)", surface, surfaceNamePattern)
	}
	return nil
}

// ItemKind discriminates what a surface has been handed. The set is closed:
// a surface that does not know what to do with one of these has a gap, and a
// gap should be a compile-time conversation rather than a silent drop.
type ItemKind string

const (
	// ItemReport is agent-written text about untrusted repository content.
	// Relay it, never act on it, and frame it as a quotation.
	ItemReport ItemKind = "report"
	// ItemNotice is one runtime fact — blocked, failed, finished. The server's
	// own words, so it carries no quotation framing.
	ItemNotice ItemKind = "notice"
	// ItemMessage is a stored conversation message: the head's reply, or
	// another surface's turn mirrored in.
	ItemMessage ItemKind = "message"
	// ItemDelta is the head's reply in progress. A surface that cannot render
	// partial text ignores it and waits for [ItemMessage].
	ItemDelta ItemKind = "delta"
	// ItemProposal is something uncontained waiting on a person, or the record
	// of it having been decided. A surface that can show cards renders it as
	// one; a blind surface says it out loud and takes the yes through the
	// read-back rule ([Surface.CanShowCards]).
	ItemProposal ItemKind = "proposal"
)

// Item is one thing the core hands a surface.
//
// This is TextInjector generalised, with the kind typed so a transport that
// cannot show a card knows it has been handed one. Exactly one of the pointer
// fields is set, and which one is [Item.Kind].
type Item struct {
	Kind ItemKind `json:"kind,omitempty"`
	// SessionID is the session this is about, where it is about one.
	SessionID string `json:"sessionId,omitempty"`
	// Report is set for [ItemReport].
	Report *Report `json:"report,omitempty"`
	// Notice is set for [ItemNotice].
	Notice *Notice `json:"notice,omitempty"`
	// Message is set for [ItemMessage].
	Message *Message `json:"message,omitempty"`
	// Delta is set for [ItemDelta].
	Delta *Delta `json:"delta,omitempty"`
	// Proposal is set for [ItemProposal], on create and on decide. A surface
	// reads [Proposal.Status] to tell one from the other: an open one is a
	// card, a decided one is a record.
	Proposal *Proposal `json:"proposal,omitempty"`
}

// Surface is anything the operator can be reached on.
//
// A surface registers with the core ([Service.RegisterSurface]) and gets three
// things: delivery of whatever happens, the verb table with the same refusals
// for every caller, and the conversation — append a turn, read the tail, and
// [Service.SinceLast].
type Surface interface {
	// Name is the surface's name, which is also its seen-mark key. It must
	// satisfy the surface name rule; a registration that does not is refused.
	Name() string

	// Deliver hands over one item. It must not block: the callers are an MCP
	// tool handler with an agent waiting, and the runtime's turn-end listener.
	Deliver(ctx context.Context, item Item) error

	// CanShowCards reports whether this surface can render a proposal card.
	// The thread can. A call cannot, and neither can a messaging line — there,
	// an uncontained proposal is accepted only through the read-back rule.
	CanShowCards() bool
}

// surfaceSet is the registered surfaces, guarded for concurrent delivery.
type surfaceSet struct {
	mu   sync.RWMutex
	live map[Surface]struct{}
}

func newSurfaceSet() *surfaceSet {
	return &surfaceSet{live: make(map[Surface]struct{})}
}

func (s *surfaceSet) add(su Surface) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.live[su] = struct{}{}
}

func (s *surfaceSet) remove(su Surface) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.live, su)
}

// snapshot copies the set so a delivery never holds the lock while a surface
// renders or speaks.
func (s *surfaceSet) snapshot() []Surface {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Surface, 0, len(s.live))
	for su := range s.live {
		out = append(out, su)
	}
	return out
}
