package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// The conversation.
//
// It is a CHANNEL — one project-less channel per assistant, the `messages`
// table as the source of truth the way it is for every channel timeline, with
// `channels.kind = 'assistant'` keeping it out of channel lists. Not a
// `sessions` row, which would have bought resume, streaming and a transcript
// for free: the memory has to be ours either way, because a Gemini call cannot
// resume a Claude CLI transcript, and a CLI-resumed history beside this one
// would be two conversation memories that drift.
//
// So the head's own context is a cache of what is here, never the memory. The
// price is that a channel message is whole where a session's turn streams,
// which is why the head's in-progress reply rides a delta push and is stored
// on completion.

// Sender vocabulary in the messages table. The conversation is a channel, so
// these are the channel's own values: the operator is `user`, the assistant is
// the third sender type `persona`, which the legacy event mirror already skips.
const (
	senderUser    = "user"
	senderPersona = "persona"
	// senderSystem is the server's own voice in the conversation: today, the
	// message the heartbeat writes to wake the head. Not `persona`, because the
	// assistant did not say it, and not `user`, because the operator did not
	// either — a turn nobody asked for has to be visibly nobody's.
	senderSystem = "system"
	// personaSenderID and personaSenderName identify the assistant in a
	// timeline that can also carry sessions and people.
	personaSenderID   = "assistant"
	personaSenderName = "Assistant"
	// conversationChannelName is what the channel is called if anything ever
	// lists it. Nothing does: kind = 'assistant' keeps it out of every channel
	// list.
	conversationChannelName = "Assistant"
	// messageTypeMessage is the channel vocabulary's ordinary message, and the
	// only type this conversation writes: a proposal is a row in
	// `assistant_proposals` rather than a message, so a card cannot go stale
	// against the decision that settled it (proposals.go). Informational types
	// are skipped by the legacy event mirror, which is why they are types
	// rather than flags.
	messageTypeMessage = "message"
)

// messageTimeFormat is the conversation's own stamp: fixed-width nanoseconds,
// so lexicographic order is chronological order down to the last digit.
//
// Sharper than the seconds every other assistant timestamp uses, and sharper
// than the messages table's own millisecond default, because this column is
// also the history cursor: two turns written in one millisecond would page and
// render in uuid order, which is to say in no order.
const messageTimeFormat = "2006-01-02T15:04:05.000000000Z"

func formatMessageTime(t time.Time) string { return t.UTC().Format(messageTimeFormat) }

// metadataSurfaceKey and metadataCallKey are how a message says where it was
// said. A voice call mirrors its turns into the same conversation, so "what
// did I agree to on the drive" is in the thread.
const (
	metadataSurfaceKey = "surface"
	metadataCallKey    = "callId"
	// metadataKindKey marks a message that is not an ordinary turn in the
	// conversation. Two things carry one: the digest, which is composed rather
	// than said and renders as a panel, and both halves of a heartbeat turn,
	// which render as a divider and a marked reply (docs/assistant.md, the M4
	// contract).
	metadataKindKey = "kind"
)

// Roles on the wire. The store's sender types are a channel's vocabulary; a
// reader of the conversation wants the two roles a conversation has.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	// RoleSystem is the server's own: the heartbeat's message saying what woke
	// the assistant. A surface renders it as a divider rather than a bubble,
	// because nobody said it.
	RoleSystem = "system"
)

// Message is one turn of the conversation on the wire.
type Message struct {
	ID   string `json:"id,omitempty"`
	Role string `json:"role,omitempty"`
	Text string `json:"text,omitempty"`
	// Surface is where it was said — "thread", "voice" — where the writer said.
	Surface string `json:"surface,omitempty"`
	// CallID is the voice call a mirrored turn came from.
	CallID string `json:"callId,omitempty"`
	// Kind is empty for a turn in the conversation, "digest" for a digest, which
	// is composed rather than said, and "heartbeat" for both halves of a turn
	// the heartbeat started. A surface that does not know a kind renders it as
	// an ordinary message, which is what it also reads as.
	Kind string `json:"kind,omitempty"`
	// CreatedAt is the messages table's own stamp, which is RFC3339 with
	// fractional seconds — that table predates the seconds rule, and it is
	// also the history cursor, so it is passed through rather than reformatted.
	CreatedAt string `json:"createdAt,omitempty"`
}

// Delta is the head's reply in progress.
//
// The price of the conversation being a channel rather than a session: a
// channel message is whole where a session's turn streams, so the in-progress
// text rides this push and the finished text is stored as a [Message].
type Delta struct {
	// Surface is the surface whose ask is being answered.
	Surface string `json:"surface,omitempty"`
	// Text is the new text, not the whole reply so far.
	Text string `json:"text,omitempty"`
}

// EnsureConversation returns the conversation channel's id, creating it on
// first use.
//
// Three steps, in this order, because each is a fallback for the one before:
// the cached id, the state row's id, and the oldest channel with
// kind = 'assistant'. Only then is one created. The last fallback is what
// stops a lost state row orphaning a conversation that still holds every
// message.
func (s *Service) EnsureConversation(ctx context.Context) (string, error) {
	s.convMu.Lock()
	defer s.convMu.Unlock()

	if s.channelID != "" {
		return s.channelID, nil
	}

	if state, err := s.store.GetAssistantState(ctx); err == nil && state.ChannelID != "" {
		s.channelID = state.ChannelID
		return s.channelID, nil
	}

	if ch, err := s.store.GetAssistantChannel(ctx); err == nil && ch.ID != "" {
		s.channelID = ch.ID
		s.recordChannel(ctx, ch.ID)
		return s.channelID, nil
	}

	ch, err := s.store.CreateAssistantChannel(ctx, store.CreateAssistantChannelParams{
		ID:   uuid.New().String(),
		Name: conversationChannelName,
	})
	if err != nil {
		return "", fmt.Errorf("create assistant conversation: %w", err)
	}
	s.channelID = ch.ID
	s.recordChannel(ctx, ch.ID)
	s.log.Info("assistant conversation created", "channel", ch.ID)
	return s.channelID, nil
}

// conversationID finds the conversation WITHOUT creating it, answering false
// when there is not one yet.
//
// Every read goes through this rather than through [Service.EnsureConversation]:
// a first `assistant.history` on a fresh database would otherwise insert the
// channel row and stamp the state row, and that op is on the socket's read
// lane, whose membership is the claim that the handler mutates nothing a later
// request could observe out of order. Creation belongs to the writers —
// appending a message, mirroring a call's turn — which are on the mutation
// lane where a write is what they are for.
//
// The two fallbacks are [Service.EnsureConversation]'s, minus the state-row
// stamp: caching the id in memory is not a write anybody else can observe.
func (s *Service) conversationID(ctx context.Context) (string, bool) {
	s.convMu.Lock()
	defer s.convMu.Unlock()

	if s.channelID != "" {
		return s.channelID, true
	}
	if state, err := s.store.GetAssistantState(ctx); err == nil && state.ChannelID != "" {
		s.channelID = state.ChannelID
		return s.channelID, true
	}
	if ch, err := s.store.GetAssistantChannel(ctx); err == nil && ch.ID != "" {
		s.channelID = ch.ID
		return s.channelID, true
	}
	return "", false
}

// recordChannel stamps the state row. A failure here is logged rather than
// returned: the channel exists and is findable by kind, so the next boot
// recovers, and refusing the message the operator just sent would be a worse
// answer than a missing cache.
func (s *Service) recordChannel(ctx context.Context, channelID string) {
	err := s.store.SetAssistantChannel(ctx, store.SetAssistantChannelParams{
		ChannelID: channelID,
		Now:       formatTime(s.now()),
	})
	if err != nil {
		s.log.Warn("assistant state not stamped with its channel", "channel", channelID, "error", err)
	}
}

// Say is a transport's whole contract: the operator's text in, the head's
// reply out.
//
// The user's turn is stored FIRST and broadcast before the head runs, because
// the reply takes seconds to minutes and a thread that shows nothing until it
// lands looks like a dropped message. Deltas ride [EventDelta] while the turn
// runs; the reply is stored whole and pushed as [EventMessage].
//
// One turn at a time, per head: [Service.runHeadTurn] serialises, so two
// surfaces cannot interleave one CLI transcript.
func (s *Service) Say(ctx context.Context, surface, text string) (Message, error) {
	said, _, err := s.beginSay(ctx, surface, text)
	if err != nil {
		return Message{}, err
	}
	return s.answer(ctx, surface, said)
}

// SayAsync stores the ask, starts the turn, and returns the ask.
//
// This is the one a socket RPC should call. A head turn takes seconds to
// minutes, and `dispatchLoop` runs a mutation SERIALLY in arrival order — so a
// blocking say holds that connection's whole mutation lane for the length of
// the turn, with every later mutation queued behind it. Nothing is lost by
// answering early: the ask is already stored and pushed, the reply arrives on
// [EventDelta] and then [EventMessage], and the thread renders from those
// pushes rather than from the RPC's reply.
//
// The turn runs on a background context, not the caller's: a request context
// is cancelled the moment the RPC answers, which would kill the turn it just
// started.
func (s *Service) SayAsync(ctx context.Context, surface, text string) (Message, error) {
	said, ask, err := s.beginSay(ctx, surface, text)
	if err != nil {
		return Message{}, err
	}

	go func() {
		// A failure is not lost with this goroutine: [Service.answer] stores the
		// assistant's own sentence about it, which is what the surface renders
		// and what releases a composer waiting on a reply. The error itself is a
		// log line, because nothing here can act on it.
		if _, err := s.answer(context.Background(), surface, said); err != nil {
			s.log.Warn("assistant turn failed", "surface", surface, "error", err)
		}
	}()
	return ask, nil
}

// beginSay validates and stores the operator's turn.
//
// It happens before the head runs, and it is pushed before the head runs,
// because the reply takes seconds to minutes and a thread that shows nothing
// until it lands looks like a dropped message. It also means a head that
// cannot start loses the turn and not the message.
func (s *Service) beginSay(ctx context.Context, surface, text string) (said string, ask Message, err error) {
	if err := checkSurface(surface); err != nil {
		return "", Message{}, err
	}
	said = strings.TrimSpace(text)
	if said == "" {
		return "", Message{}, errors.New("assistant: nothing to say")
	}

	ask, err = s.appendMessage(ctx, senderUser, surface, "", "", said)
	if err != nil {
		return "", Message{}, err
	}
	return said, ask, nil
}

// What the assistant says when it has nothing of its own to say. Its own words,
// so they are not untrusted and not a quotation.
//
// Both cases have to produce a message, because a stored assistant turn is what
// a surface waits for: the thread arms its composer when the ask goes out and
// releases it on the reply, so a turn that ends in silence leaves the operator
// looking at their own message with the field shut and nothing to read. The
// detail goes to the log, not here — the reason a CLI failed is not a sentence
// anybody can act on, where "try again" is.
const (
	turnFailedText = "I did not manage to answer that — something went wrong on my side and the " +
		"turn ended early. Your message is saved, so try again. If it keeps happening, the server " +
		"log has the detail."
	turnSilentText = "I ran that turn without writing anything back. Ask again if you were " +
		"expecting an answer."
)

// answer runs one head turn and stores what it said.
//
// Every path through here stores exactly one assistant message: what the head
// wrote, or the server's own sentence about why there is nothing. A turn that
// ends without one is indistinguishable, from every surface, from a turn that
// is still running.
func (s *Service) answer(ctx context.Context, surface, said string) (Message, error) {
	reply, err := s.runHeadTurn(ctx, surface, said)
	if err != nil {
		s.note(ctx, surface, turnFailedText)
		return Message{}, err
	}
	if strings.TrimSpace(reply) == "" {
		// Not an error: the head may have done nothing but call verbs. It still
		// owes the surface a turn.
		return s.note(ctx, surface, turnSilentText), nil
	}

	stored, err := s.appendMessage(ctx, senderPersona, surface, "", "", reply)
	if err != nil {
		return Message{}, err
	}
	s.deliver(ctx, Item{Kind: ItemMessage, Message: &stored})
	return stored, nil
}

// note stores one sentence of the server's own as the assistant's turn.
//
// A failure to store it is logged rather than raised: the caller is already
// handling one failure, and there is nothing better to do with a second.
func (s *Service) note(ctx context.Context, surface, text string) Message {
	stored, err := s.appendMessage(ctx, senderPersona, surface, "", "", text)
	if err != nil {
		s.log.Warn("assistant could not store its own note", "surface", surface, "error", err)
		return Message{}
	}
	s.deliver(ctx, Item{Kind: ItemMessage, Message: &stored})
	return stored
}

// Mirror writes a turn that happened somewhere else into the conversation.
//
// A voice call's turns land here with metadata.surface = "voice" and the call
// id, which is what makes the thread the record of a drive and the drive's
// record the next call's greeting. role is [RoleUser] or [RoleAssistant];
// anything else is refused rather than guessed at, because a mirrored turn
// that lands under the wrong speaker is a conversation nobody can read.
func (s *Service) Mirror(ctx context.Context, surface, callID, role, text string) (Message, error) {
	if err := checkSurface(surface); err != nil {
		return Message{}, err
	}
	said := strings.TrimSpace(text)
	if said == "" {
		return Message{}, errors.New("assistant: nothing to mirror")
	}

	var sender string
	switch role {
	case RoleUser:
		sender = senderUser
	case RoleAssistant:
		sender = senderPersona
	default:
		return Message{}, fmt.Errorf("assistant: %q is not a role (want %q or %q)", role, RoleUser, RoleAssistant)
	}

	stored, err := s.appendMessage(ctx, sender, surface, callID, "", said)
	if err != nil {
		return Message{}, err
	}
	return stored, nil
}

// appendMessage stores one conversation message and pushes it.
func (s *Service) appendMessage(ctx context.Context, senderType, surface, callID, kind, text string) (Message, error) {
	channelID, err := s.EnsureConversation(ctx)
	if err != nil {
		return Message{}, err
	}

	metadata := map[string]string{metadataSurfaceKey: surface}
	if callID != "" {
		metadata[metadataCallKey] = callID
	}
	if kind != "" {
		metadata[metadataKindKey] = kind
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return Message{}, fmt.Errorf("encode message metadata: %w", err)
	}

	senderID := ""
	senderName := ""
	if senderType == senderPersona {
		senderID, senderName = personaSenderID, personaSenderName
	}

	row, err := s.store.InsertAssistantMessage(ctx, store.InsertAssistantMessageParams{
		ID:          uuid.New().String(),
		ChannelID:   channelID,
		SenderType:  senderType,
		SenderID:    senderID,
		SenderName:  senderName,
		Content:     text,
		MessageType: messageTypeMessage,
		Metadata:    string(encoded),
		CreatedAt:   formatMessageTime(s.now()),
	})
	if err != nil {
		return Message{}, fmt.Errorf("append conversation message: %w", err)
	}

	msg := messageFrom(row)
	s.broadcast(EventMessage, msg)
	return msg, nil
}

// Page is one page of the conversation.
type Page struct {
	// Messages is oldest first: a transcript reads in the order it was said,
	// and a preamble cannot be pasted in any other order.
	Messages []Message `json:"messages,omitempty"`
	// Before is the cursor for the page BEFORE this one, or "" at the start of
	// the conversation.
	//
	// Opaque, and a (timestamp, id) PAIR rather than a timestamp: a page
	// boundary that compares only the stamp skips every row that shares the
	// boundary's stamp. The conversation's own writes are nanosecond-stamped so
	// a tie is close to impossible, which is exactly why the cursor should not
	// depend on that being true.
	Before string `json:"before,omitempty"`
}

// History returns a page of the conversation, oldest first, ending at before.
//
// before is a cursor from a previous page ([Page.Before]) and "" starts at the
// newest message, so paging backwards is "hand me back the cursor you were
// given". A malformed cursor is an error rather than a silent jump to the
// newest page: a client that loops on the newest page never reaches the start.
func (s *Service) History(ctx context.Context, before string, limit int) (Page, error) {
	beforeAt, beforeID, err := decodeCursor(before)
	if err != nil {
		return Page{}, err
	}

	// No conversation yet is an empty one. Reading must not create it: this is a
	// read-lane op, and the first visit to the thread on a fresh machine would
	// otherwise write two rows.
	channelID, found := s.conversationID(ctx)
	if !found {
		return Page{}, nil
	}

	size := clampLimit(limit, maxHistoryPage)
	rows, err := s.store.ListAssistantMessagesBefore(ctx, store.ListAssistantMessagesBeforeParams{
		ChannelID: channelID,
		BeforeAt:  beforeAt,
		BeforeID:  beforeID,
		Lim:       int64(size),
	})
	if err != nil {
		return Page{}, fmt.Errorf("read conversation: %w", err)
	}

	page := Page{Messages: make([]Message, 0, len(rows))}
	for i := len(rows) - 1; i >= 0; i-- {
		page.Messages = append(page.Messages, messageFrom(rows[i]))
	}
	// A full page may have more behind it; a short one is the start.
	if len(rows) == size && len(page.Messages) > 0 {
		page.Before = encodeCursor(page.Messages[0])
	}
	return page, nil
}

// cursorSeparator is not legal in either half of a cursor: a timestamp has no
// pipe and a uuid has none either, so splitting on it cannot be ambiguous.
const cursorSeparator = "|"

func encodeCursor(msg Message) string { return msg.CreatedAt + cursorSeparator + msg.ID }

// decodeCursor splits a cursor. An empty one is the newest page.
func decodeCursor(cursor string) (at, id string, err error) {
	if cursor == "" {
		return "", "", nil
	}
	at, id, found := strings.Cut(cursor, cursorSeparator)
	if !found || at == "" || id == "" {
		return "", "", fmt.Errorf("%q is not a conversation cursor", cursor)
	}
	return at, id, nil
}

// messageFrom maps a stored row to the wire shape.
func messageFrom(row store.Message) Message {
	msg := Message{
		ID:        row.ID,
		Text:      row.Content,
		CreatedAt: row.CreatedAt,
		Role:      RoleUser,
	}
	switch row.SenderType {
	case senderPersona:
		msg.Role = RoleAssistant
	case senderSystem:
		msg.Role = RoleSystem
	}
	if row.Metadata != "" {
		var metadata map[string]string
		if err := json.Unmarshal([]byte(row.Metadata), &metadata); err == nil {
			msg.Surface = metadata[metadataSurfaceKey]
			msg.CallID = metadata[metadataCallKey]
			msg.Kind = metadata[metadataKindKey]
		}
	}
	return msg
}
