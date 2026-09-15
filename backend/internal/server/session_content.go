package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"

	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/content"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/peerlink"
	"github.com/mdjarv/agentique/backend/internal/session"
)

// sessionContent answers the session content routes for every session the
// operator can see, wherever it runs (docs/multi-machine.md, "Session files").
//
// An agent links its files as /api/sessions/{id}/files/name, relative, so the
// browser always asks the server that served the page — which, for a session
// on a paired machine, is not the one holding the file. Asking that machine
// directly cannot work for anything the browser navigates to (a new tab, a
// download, an <img>), since a navigation carries no bearer, and the machine
// may not present a certificate the browser accepts anyway. So this server
// resolves the owner and relays: its own database first, then the paired
// machines' session lists, then nothing.
//
// The relayed item is served by this server's content.Serve, which judges the
// headers by name. A file's name is taken from the path this server was asked
// for, never from the owner, so an owner on another release — or a hostile
// one — cannot choose how a file renders on this origin.
type sessionContent struct {
	local session.LocalContent
	// isLocal reports whether this machine's database has the session.
	isLocal func(ctx context.Context, sessionID string) (bool, error)
	// locate finds the paired machine that owns a session.
	locate func(ctx context.Context, sessionID string) (machineID string, ok bool)
	remote remoteContent
}

// remoteContent is the peer client's half of the relay.
type remoteContent interface {
	SessionFile(ctx context.Context, machineID, sessionID, rel string) (content.Item, error)
	EventImage(ctx context.Context, machineID, sessionID string, eventID int64, idx int) (content.Item, error)
}

// SessionFile implements session.ContentSource.
func (c *sessionContent) SessionFile(ctx context.Context, sessionID, rel string) (content.Item, error) {
	if uuid.Validate(sessionID) != nil {
		return content.Item{}, content.ErrInvalidPath
	}
	if err := content.CheckName(rel); err != nil {
		return content.Item{}, err
	}
	machineID, local, err := c.owner(ctx, sessionID)
	if err != nil {
		return content.Item{}, err
	}
	if local {
		return c.local.SessionFile(ctx, sessionID, rel)
	}
	item, err := c.remote.SessionFile(ctx, machineID, sessionID, rel)
	if err != nil {
		return content.Item{}, relayError(err)
	}
	item.Name = path.Base(rel)
	return item, nil
}

// EventImage implements session.ContentSource.
func (c *sessionContent) EventImage(ctx context.Context, sessionID string, eventID int64, idx int) (content.Item, error) {
	machineID, local, err := c.owner(ctx, sessionID)
	if err != nil {
		return content.Item{}, err
	}
	if local {
		return c.local.EventImage(ctx, sessionID, eventID, idx)
	}
	item, err := c.remote.EventImage(ctx, machineID, sessionID, eventID, idx)
	if err != nil {
		return content.Item{}, relayError(err)
	}
	// The owner named the image, and only its extension is read here — and
	// read through this server's allowlist, so the most a wrong one can do is
	// render an inert type or force a download.
	return item, nil
}

// owner resolves where a session lives: here, on a paired machine, or nowhere
// this server knows of.
func (c *sessionContent) owner(ctx context.Context, sessionID string) (machineID string, local bool, err error) {
	local, err = c.isLocal(ctx, sessionID)
	if err != nil {
		return "", false, fmt.Errorf("look up session: %w", err)
	}
	if local {
		return "", true, nil
	}
	if c.locate == nil || c.remote == nil {
		return "", false, content.ErrNotFound
	}
	machineID, ok := c.locate(ctx, sessionID)
	if !ok {
		return "", false, content.ErrNotFound
	}
	return machineID, false, nil
}

// relayError maps the peer client's failures onto content's sentinels. The
// owner's own refusal keeps its meaning; everything else is either a release
// that cannot relay or a machine that did not answer.
func relayError(err error) error {
	var refusal *peerlink.RefusalError
	switch {
	case errors.As(err, &refusal) && refusal.Reason == peer.ReasonNotFound:
		return content.ErrNotFound
	case errors.As(err, &refusal) && refusal.Reason == peer.ReasonBadRequest:
		return content.ErrInvalidPath
	case errors.Is(err, content.ErrTooLarge):
		return err
	case errors.Is(err, peerlink.ErrNoPeerSurface):
		return fmt.Errorf("%w: %w", content.ErrUnsupported, err)
	default:
		return fmt.Errorf("%w: %w", content.ErrUnavailable, err)
	}
}

// localSession is isLocal over this machine's database.
func localSession(get func(ctx context.Context, id string) error) func(ctx context.Context, id string) (bool, error) {
	return func(ctx context.Context, id string) (bool, error) {
		err := get(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return err == nil, err
	}
}
