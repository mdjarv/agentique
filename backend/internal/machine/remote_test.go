package machine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// fakeRemote stands in for a paired machine: it proves its pinned identity
// honestly and answers the revoke with whatever status the test is about.
type fakeRemote struct {
	server       *httptest.Server
	revokeStatus int
	revokeCalls  int
}

func newFakeRemote(t *testing.T, machineID string, identity *SigningIdentity, revokeStatus int) *fakeRemote {
	t.Helper()
	remote := &fakeRemote{revokeStatus: revokeStatus}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agentique/environment", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]string{
			"machineId":   machineID,
			"identityKey": identity.PublicKey(),
		})
	})
	mux.HandleFunc("POST /api/auth/identity-proof", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Nonce string `json:"nonce"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode challenge: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		proof, err := identity.SignChallenge(body.Nonce)
		if err != nil {
			t.Errorf("sign challenge: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(t, w, map[string]string{
			"machineId":   machineID,
			"identityKey": identity.PublicKey(),
			"proof":       proof,
		})
	})
	mux.HandleFunc("DELETE /api/auth/session", func(w http.ResponseWriter, _ *http.Request) {
		remote.revokeCalls++
		w.WriteHeader(remote.revokeStatus)
	})

	remote.server = httptest.NewServer(mux)
	t.Cleanup(remote.server.Close)
	return remote
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func newTestIdentity(t *testing.T) (string, *SigningIdentity) {
	t.Helper()
	machineID := uuid.New().String()
	identity, err := LoadOrCreateSigningIdentity(t.TempDir(), machineID)
	if err != nil {
		t.Fatalf("create signing identity: %v", err)
	}
	return machineID, identity
}

func TestRevokeRemoteBearerSucceeds(t *testing.T) {
	machineID, identity := newTestIdentity(t)
	remote := newFakeRemote(t, machineID, identity, http.StatusNoContent)

	err := RevokeRemoteBearer(context.Background(), remote.server.Client(),
		remote.server.URL, machineID, identity.PublicKey(), "live-token")
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if remote.revokeCalls != 1 {
		t.Fatalf("revoke calls = %d, want 1", remote.revokeCalls)
	}
}

// A credential the remote already refuses is a credential already revoked.
// Failing here stranded the catalog entry: the machine could be neither used
// nor removed, which is exactly the state a wiped session table leaves behind.
func TestRevokeRemoteBearerTreatsRefusalAsRevoked(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		machineID, identity := newTestIdentity(t)
		remote := newFakeRemote(t, machineID, identity, status)

		err := RevokeRemoteBearer(context.Background(), remote.server.Client(),
			remote.server.URL, machineID, identity.PublicKey(), "dead-token")
		if err != nil {
			t.Fatalf("status %d: revoke returned %v, want nil", status, err)
		}
		if remote.revokeCalls != 1 {
			t.Fatalf("status %d: revoke calls = %d, want 1", status, remote.revokeCalls)
		}
	}
}

// Tolerating a refusal must not become "removal never checks anything": a
// server error is still a failure, and the entry stays.
func TestRevokeRemoteBearerFailsOnServerError(t *testing.T) {
	machineID, identity := newTestIdentity(t)
	remote := newFakeRemote(t, machineID, identity, http.StatusInternalServerError)

	err := RevokeRemoteBearer(context.Background(), remote.server.Client(),
		remote.server.URL, machineID, identity.PublicKey(), "live-token")
	if err == nil {
		t.Fatal("revoke succeeded on a 500, want an error")
	}
}

// The identity proof gates the revoke, so a machine answering with someone
// else's identity never receives the credential at all.
func TestRevokeRemoteBearerRejectsIdentityMismatch(t *testing.T) {
	machineID, identity := newTestIdentity(t)
	remote := newFakeRemote(t, machineID, identity, http.StatusNoContent)

	_, other := newTestIdentity(t)
	err := RevokeRemoteBearer(context.Background(), remote.server.Client(),
		remote.server.URL, machineID, other.PublicKey(), "live-token")
	if err == nil {
		t.Fatal("revoke succeeded against a mismatched identity pin")
	}
	if remote.revokeCalls != 0 {
		t.Fatalf("revoke calls = %d, want 0 — credential sent before the proof passed", remote.revokeCalls)
	}
}
