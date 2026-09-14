// Package peerlink is the acting server's client for a paired machine's peer
// surface (docs/peers.md): it holds the peer credential, and it is the one place
// this server calls another machine's /api/peer/* routes.
//
// It is the counterpart of internal/peer, which serves those routes and never
// dials out. Every call proves the machine's pinned identity before the
// credential leaves (machine.DoRemoteJSON), and the credential it presents is
// always the peer one: the full bearer the catalog holds is used for exactly
// one thing here, minting that peer credential. A peer credential that stops
// being accepted is rotated once with the bearer and the call retried; a
// machine whose release has no peer surface answers [ErrNoPeerSurface], and
// nothing falls back to driving it with the bearer.
package peerlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/store"
)

const (
	maxListBytes       = 8 << 20
	maxSmallBytes      = 256 << 10
	maxTranscriptBytes = 256 << 10
)

// ErrNoPeerSurface is a paired machine whose release predates the peer surface.
// It can be listed the old way and acted on not at all.
var ErrNoPeerSurface = errors.New("that machine's release does not serve the peer surface")

// ErrNotPaired is a catalog row with no credential to mint from.
var ErrNotPaired = errors.New("that machine must be re-paired before this server can act on it")

// RefusalError is the owner's guard saying no, with its stable reason.
type RefusalError struct {
	Status   int
	Reason   string
	Message  string
	Families []string
}

func (e *RefusalError) Error() string {
	return fmt.Sprintf("refused (%s): %s", e.Reason, e.Message)
}

// Catalog is the machine rows this client reads and the peer columns it writes.
type Catalog interface {
	GetMachine(ctx context.Context, machineID string) (store.Machine, error)
	SetMachinePeerCredential(ctx context.Context, arg store.SetMachinePeerCredentialParams) (int64, error)
}

// Client talks to paired machines' peer surfaces.
type Client struct {
	http    *http.Client
	catalog Catalog
	label   func(ctx context.Context) string

	mu    sync.Mutex
	mints map[string]*sync.Mutex
}

// Option configures a [Client].
type Option func(*Client)

// WithLabel names this server in the credential label a remote lists, read per
// mint so a rename shows on the next one.
func WithLabel(label func(ctx context.Context) string) Option {
	return func(c *Client) { c.label = label }
}

// New builds a client. httpClient must not follow redirects (the server's
// machine client does not). It does no IO.
func New(httpClient *http.Client, catalog Catalog, opts ...Option) *Client {
	c := &Client{http: httpClient, catalog: catalog, mints: make(map[string]*sync.Mutex)}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// List reads the machine's sessions, projects and opt-ins.
func (c *Client) List(ctx context.Context, machineID string) (peer.SessionsResponse, error) {
	var out peer.SessionsResponse
	err := c.do(ctx, machineID, http.MethodGet, "/api/peer/sessions", nil, maxListBytes, &out)
	return out, err
}

// Create asks the machine to create a session, and to send its first prompt
// in the same call when req carries one.
func (c *Client) Create(ctx context.Context, machineID string, req peer.CreateRequest) (peer.CreateResponse, error) {
	var out peer.CreateResponse
	err := c.do(ctx, machineID, http.MethodPost, "/api/peer/sessions", req, maxSmallBytes, &out)
	return out, err
}

// Send asks the machine to deliver a prompt to one of its sessions.
func (c *Client) Send(ctx context.Context, machineID, sessionID string, req peer.SendRequest) (peer.SendResponse, error) {
	var out peer.SendResponse
	err := c.do(ctx, machineID, http.MethodPost, "/api/peer/sessions/"+url.PathEscape(sessionID)+"/send",
		req, maxSmallBytes, &out)
	return out, err
}

// Follow subscribes this server to a session's news on that machine.
func (c *Client) Follow(ctx context.Context, machineID, sessionID string) error {
	return c.do(ctx, machineID, http.MethodPost, "/api/peer/sessions/"+url.PathEscape(sessionID)+"/follow",
		struct{}{}, maxSmallBytes, nil)
}

// Transcript reads a session's recent transcript: agent-written data.
func (c *Client) Transcript(ctx context.Context, machineID, sessionID string) (string, error) {
	var out peer.TranscriptResponse
	err := c.do(ctx, machineID, http.MethodGet, "/api/peer/sessions/"+url.PathEscape(sessionID)+"/transcript",
		nil, maxTranscriptBytes, &out)
	return out.Transcript, err
}

// Events polls the machine's outbox for this server after since, waiting up to
// wait for news. The caller's context must outlive wait.
func (c *Client) Events(ctx context.Context, machineID string, since int64, wait time.Duration) (peer.EventsResponse, error) {
	q := url.Values{}
	q.Set("since", strconv.FormatInt(since, 10))
	q.Set("wait", strconv.Itoa(int(wait/time.Second)))
	var out peer.EventsResponse
	err := c.do(ctx, machineID, http.MethodGet, "/api/peer/events?"+q.Encode(), nil, maxListBytes, &out)
	return out, err
}

// do runs one call with the peer credential, rotating it once if the machine
// no longer accepts it.
func (c *Client) do(ctx context.Context, machineID, method, path string, body any, maxBytes int64, dst any) error {
	m, token, err := c.credential(ctx, machineID, "")
	if err != nil {
		return err
	}
	err = c.call(ctx, m, token, method, path, body, maxBytes, dst)
	var status *machine.RemoteStatusError
	if !errors.As(err, &status) || status.Status != http.StatusUnauthorized {
		return err
	}
	// Refused credential: mint a replacement, naming the old one so it is
	// deleted rather than left alive, and try exactly once more.
	m, token, err = c.credential(ctx, machineID, token)
	if err != nil {
		return err
	}
	return c.call(ctx, m, token, method, path, body, maxBytes, dst)
}

func (c *Client) call(ctx context.Context, m store.Machine, token, method, path string, body any, maxBytes int64, dst any) error {
	remote := machine.RemotePeer{BaseURL: m.BaseUrl, MachineID: m.MachineID, IdentityKey: m.IdentityKey, Token: token}
	err := machine.DoRemoteJSON(ctx, c.http, remote, method, path, body, maxBytes, dst)
	return classify(err)
}

// classify turns an owner's refusal body into a [*RefusalError] and a route
// the machine does not have into [ErrNoPeerSurface].
func classify(err error) error {
	var status *machine.RemoteStatusError
	if !errors.As(err, &status) {
		return err
	}
	var body peer.ErrorResponse
	if json.Unmarshal(status.Body, &body) == nil && body.Reason != "" {
		return &RefusalError{Status: status.Status, Reason: body.Reason, Message: body.Error, Families: body.Families}
	}
	if status.Status == http.StatusNotFound || status.Status == http.StatusMethodNotAllowed {
		return fmt.Errorf("%w (status %d)", ErrNoPeerSurface, status.Status)
	}
	return err
}

// credential returns the machine row and its peer token, minting one when there
// is none or when stale names a token the machine just refused.
//
// One mint at a time per machine, re-reading the row inside the lock: two
// concurrent calls that both saw no token must not mint two credentials.
func (c *Client) credential(ctx context.Context, machineID, stale string) (store.Machine, string, error) {
	lock := c.mintLock(machineID)
	lock.Lock()
	defer lock.Unlock()

	m, err := c.catalog.GetMachine(ctx, machineID)
	if err != nil {
		return store.Machine{}, "", fmt.Errorf("machine %s: %w", machineID, err)
	}
	if m.PeerToken != "" && m.PeerToken != stale {
		return m, m.PeerToken, nil
	}
	if m.Token == "" || m.IdentityKey == "" {
		return m, "", ErrNotPaired
	}

	label := "paired server"
	if c.label != nil {
		if l := c.label(ctx); l != "" {
			label = "peer: " + l
		}
	}
	req := map[string]string{"label": label}
	if m.PeerSessionID != "" {
		req["replaceSessionId"] = m.PeerSessionID
	}
	var minted struct {
		Token     string `json:"token"`
		SessionID string `json:"sessionId"`
	}
	bearer := machine.RemotePeer{BaseURL: m.BaseUrl, MachineID: m.MachineID, IdentityKey: m.IdentityKey, Token: m.Token}
	if err := machine.DoRemoteJSON(ctx, c.http, bearer, http.MethodPost, "/api/auth/peer-credential",
		req, maxSmallBytes, &minted); err != nil {
		return m, "", fmt.Errorf("mint peer credential on %s: %w", machineID, classify(err))
	}
	if minted.Token == "" {
		return m, "", fmt.Errorf("mint peer credential on %s: empty answer", machineID)
	}
	if _, err := c.catalog.SetMachinePeerCredential(ctx, store.SetMachinePeerCredentialParams{
		PeerToken: minted.Token, PeerSessionID: minted.SessionID, MachineID: machineID,
	}); err != nil {
		return m, "", fmt.Errorf("store peer credential for %s: %w", machineID, err)
	}
	m.PeerToken, m.PeerSessionID = minted.Token, minted.SessionID
	return m, minted.Token, nil
}

func (c *Client) mintLock(machineID string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	lock, ok := c.mints[machineID]
	if !ok {
		lock = &sync.Mutex{}
		c.mints[machineID] = lock
	}
	return lock
}
