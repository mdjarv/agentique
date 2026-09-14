package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	pathpkg "path"
	"strings"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// KindPeer is the auth session kind a paired SERVER presents when it acts on
// this one (docs/peers.md). It exists so an unattended server-side loop never
// runs on the credential a browser uses, and so peer actions can be revoked
// without unpairing the machine.
const KindPeer = "peer"

// PeerRoutePrefix is the route family a peer credential is for.
const PeerRoutePrefix = "/api/peer/"

// errCredentialScope is what a credential presented outside its routes gets.
var errCredentialScope = errors.New("credential is not valid for this route")

// credentialAllowed is the one scope rule for every credential kind.
//
// A peer credential is accepted on the peer surface and on revoking itself.
// Nothing else — not a ws-ticket (the peer surface has no socket; its event feed
// is a long poll that carries the credential in a header), not the session
// API, not the machine catalog, not the files, not pairing. Every other
// kind is refused on the peer surface, because a peer route stamps what it does
// as the assistant's, and that claim comes from the credential rather than the
// request body.
//
// A peer path must also be written exactly as it will be routed: clean, and
// with nothing percent-encoded. The check reads the decoded path while the mux
// matches the escaped one and redirects an unclean one, so "/api/peer/../x"
// would pass a prefix test here and name a different route there.
func credentialAllowed(kind string, r *http.Request) bool {
	path := r.URL.Path
	if kind != KindPeer {
		return !strings.HasPrefix(path, PeerRoutePrefix)
	}
	if path == "/api/auth/session" {
		return true
	}
	if !strings.HasPrefix(path, PeerRoutePrefix) {
		return false
	}
	return pathpkg.Clean(path) == path && r.URL.EscapedPath() == path
}

// maxPeerLabelRunes bounds the label a peer credential is listed under.
const maxPeerLabelRunes = 128

// handleMintPeerCredential mints the credential a paired server uses to act on
// this one. POST /api/auth/peer-credential {label, replaceSessionId?}.
//
// Authorized like pairing: an admin session or the admin secret. In practice the
// caller is the acting server holding the bearer this machine gave it at
// pairing, asking once and storing the answer. A peer credential cannot mint
// another — the scope rule refuses it before this handler sees it.
//
// replaceSessionId rotates: the named peer credential is deleted in the same
// call, so a server that re-requests never leaves its old one alive. It can
// only name a PEER credential of the same user; a browser's session is never
// reachable through it.
func (s *Service) handleMintPeerCredential(w http.ResponseWriter, r *http.Request) {
	userID, _, err := s.requireAdmin(r)
	if err != nil {
		httperror.RespondError(w, httperror.Unauthorized(err.Error()))
		return
	}

	var req struct {
		Label            string `json:"label"`
		ReplaceSessionID string `json:"replaceSessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperror.RespondError(w, httperror.BadRequest("invalid request body"))
		return
	}
	label := strings.TrimSpace(req.Label)
	if len([]rune(label)) > maxPeerLabelRunes {
		httperror.RespondError(w, httperror.BadRequest("label must be 128 characters or fewer"))
		return
	}
	if len(req.ReplaceSessionID) > maxAuthSessionIDBytes {
		httperror.RespondError(w, httperror.BadRequest("replaceSessionId is too long"))
		return
	}
	if label == "" {
		label = "paired server"
	}

	token, sessionID, err := s.createSessionWithID(r.Context(), userID, label, KindPeer)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("create peer credential", err))
		return
	}
	if req.ReplaceSessionID != "" {
		n, err := s.queries.DeletePeerAuthSessionByIDAndUser(r.Context(), store.DeletePeerAuthSessionByIDAndUserParams{
			ID: sql.NullString{String: req.ReplaceSessionID, Valid: true}, UserID: userID,
		})
		if err != nil {
			s.rollbackPairedSession(r.Context(), token)
			httperror.RespondError(w, httperror.Internal("rotate previous peer credential", err))
			return
		}
		if n > 0 {
			s.closeSessionConnections(req.ReplaceSessionID)
		}
	}

	httperror.JSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"sessionId": sessionID,
		"expiresAt": time.Now().Add(sessionMaxAge).UTC().Format(time.RFC3339),
		"machineId": s.machineID,
	})
}
