package peerlink

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/mdjarv/agentique/backend/internal/content"
	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/peer"
)

const (
	// MaxContentBytes bounds one relayed file. The relay streams, so this is
	// not memory; it is how much of an owner's disk one browser request may
	// pull through this server.
	MaxContentBytes = 256 << 20
	// contentHeaderBudget bounds the identity proof and the owner's response
	// headers. The body has no deadline of its own beyond the caller's
	// context: a large file on a slow link is not a hung machine.
	contentHeaderBudget = 15 * time.Second
)

// SessionFile opens a file from a session's files directory on a paired
// machine, streaming.
func (c *Client) SessionFile(ctx context.Context, machineID, sessionID, rel string) (content.Item, error) {
	return c.openContent(ctx, machineID, "/api/peer/sessions/"+url.PathEscape(sessionID)+"/files?path="+url.QueryEscape(rel))
}

// EventImage opens a detached event image from a session on a paired machine.
func (c *Client) EventImage(ctx context.Context, machineID, sessionID string, eventID int64, idx int) (content.Item, error) {
	return c.openContent(ctx, machineID, fmt.Sprintf("/api/peer/sessions/%s/events/%d/images/%d",
		url.PathEscape(sessionID), eventID, idx))
}

// openContent streams one peer content route into an Item, rotating a refused
// credential once the way every other call here does. The Item's name is the
// owner's; a caller that knows better (a file's own path) overrides it.
func (c *Client) openContent(ctx context.Context, machineID, route string) (content.Item, error) {
	ctx, cancel := context.WithCancel(ctx)
	// Past the budget the request is abandoned; once headers are in, the
	// timer is stopped and only the caller's context bounds the body.
	timer := time.AfterFunc(contentHeaderBudget, cancel)

	resp, err := c.openWithRotation(ctx, machineID, route)
	if !timer.Stop() && err == nil {
		resp.Body.Close()
		err = context.DeadlineExceeded
	}
	if err != nil {
		cancel()
		return content.Item{}, err
	}

	// An older release answers an unmounted /api/ path with the SPA's 200.
	// Only the peer route names what it served.
	escaped := resp.Header.Get(peer.ContentNameHeader)
	name, nameErr := url.PathUnescape(escaped)
	if escaped == "" || nameErr != nil {
		resp.Body.Close()
		cancel()
		return content.Item{}, fmt.Errorf("%w: %s answered without naming the content", ErrNoPeerSurface, route)
	}
	if resp.ContentLength > MaxContentBytes {
		resp.Body.Close()
		cancel()
		return content.Item{}, content.ErrTooLarge
	}
	modTime, _ := http.ParseTime(resp.Header.Get("Last-Modified"))
	body := &boundedBody{r: resp.Body, left: MaxContentBytes}
	closer := func() error {
		defer cancel()
		return resp.Body.Close()
	}
	item := content.NewItem(path.Base(name), resp.ContentLength, modTime, body, closer)
	if cc := resp.Header.Get("Cache-Control"); cc != "" {
		// Only the owner knows its bytes never change; an immutable item gets
		// this server's own cache header, never the owner's verbatim.
		item.Immutable = cacheImmutable(cc)
	}
	return item, nil
}

func (c *Client) openWithRotation(ctx context.Context, machineID, route string) (*http.Response, error) {
	m, token, err := c.credential(ctx, machineID, "")
	if err != nil {
		return nil, err
	}
	resp, err := c.openOnce(ctx, m.BaseUrl, m.MachineID, m.IdentityKey, token, route)
	var status *machine.RemoteStatusError
	if !errors.As(err, &status) || status.Status != http.StatusUnauthorized {
		return resp, classify(err)
	}
	m, token, err = c.credential(ctx, machineID, token)
	if err != nil {
		return nil, err
	}
	resp, err = c.openOnce(ctx, m.BaseUrl, m.MachineID, m.IdentityKey, token, route)
	return resp, classify(err)
}

func (c *Client) openOnce(ctx context.Context, baseURL, machineID, identityKey, token, route string) (*http.Response, error) {
	remote := machine.RemotePeer{BaseURL: baseURL, MachineID: machineID, IdentityKey: identityKey, Token: token}
	return machine.OpenRemote(ctx, c.streamClient(), remote, route, maxSmallBytes)
}

// streamClient is the machine client without its whole-request timeout, which
// would cut a large body off mid-transfer. Redirect refusal and transport are
// kept; the header budget and the caller's context take the timeout's place.
func (c *Client) streamClient() *http.Client {
	stream := *c.http
	stream.Timeout = 0
	return &stream
}

func cacheImmutable(cacheControl string) bool {
	for directive := range strings.SplitSeq(cacheControl, ",") {
		if strings.TrimSpace(directive) == "immutable" {
			return true
		}
	}
	return false
}

// boundedBody fails a body that runs past its bound instead of truncating it
// silently, since the length the owner declared may be absent.
type boundedBody struct {
	r    io.Reader
	left int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		// At the bound exactly: only more bytes are an error.
		var probe [1]byte
		if n, err := b.r.Read(probe[:]); n == 0 {
			return 0, err
		}
		return 0, fmt.Errorf("%w: relayed body exceeds %d bytes", content.ErrTooLarge, MaxContentBytes)
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.r.Read(p)
	b.left -= int64(n)
	return n, err
}
