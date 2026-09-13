package machine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// fakeRemote stands in for a paired machine: it proves its pinned identity
// honestly and answers the revoke with whatever status the test is about.
type fakeRemote struct {
	server       *httptest.Server
	revokeStatus int
	revokeCalls  int
	// fetchAuth records the Authorization header of every GET /api/sessions,
	// so a test can see whether the credential left before the proof passed.
	fetchAuth []string
	fetchBody string
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

	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		remote.fetchAuth = append(remote.fetchAuth, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer live-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(remote.fetchBody))
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

func TestFetchRemoteJSONSendsBearerAfterProof(t *testing.T) {
	machineID, identity := newTestIdentity(t)
	remote := newFakeRemote(t, machineID, identity, http.StatusNoContent)
	remote.fetchBody = `[{"id":"s1","name":"Plugin Testing"}]`

	var got []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	peer := RemotePeer{BaseURL: remote.server.URL, MachineID: machineID, IdentityKey: identity.PublicKey(), Token: "live-token"}
	if err := FetchRemoteJSON(context.Background(), remote.server.Client(), peer, "/api/sessions", 1<<20, &got); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Plugin Testing" {
		t.Fatalf("decoded %+v, want the one session", got)
	}
}

// The proof gates the read exactly as it gates a revoke: a host holding some
// other key never sees the bearer.
func TestFetchRemoteJSONRejectsIdentityMismatch(t *testing.T) {
	machineID, identity := newTestIdentity(t)
	remote := newFakeRemote(t, machineID, identity, http.StatusNoContent)
	remote.fetchBody = `[]`

	_, other := newTestIdentity(t)
	peer := RemotePeer{BaseURL: remote.server.URL, MachineID: machineID, IdentityKey: other.PublicKey(), Token: "live-token"}
	var got []any
	if err := FetchRemoteJSON(context.Background(), remote.server.Client(), peer, "/api/sessions", 1<<20, &got); err == nil {
		t.Fatal("fetch succeeded against a mismatched identity pin")
	}
	if len(remote.fetchAuth) != 0 {
		t.Fatalf("GET reached the remote %d times — credential sent before the proof passed", len(remote.fetchAuth))
	}
}

func TestFetchRemoteJSONBoundsTheBody(t *testing.T) {
	machineID, identity := newTestIdentity(t)
	remote := newFakeRemote(t, machineID, identity, http.StatusNoContent)
	remote.fetchBody = `["` + strings.Repeat("x", 2048) + `"]`

	peer := RemotePeer{BaseURL: remote.server.URL, MachineID: machineID, IdentityKey: identity.PublicKey(), Token: "live-token"}
	var got []string
	if err := FetchRemoteJSON(context.Background(), remote.server.Client(), peer, "/api/sessions", 1024, &got); err == nil {
		t.Fatal("fetch decoded a body past its bound")
	}
}

func TestFetchRemoteJSONReportsRefusedCredential(t *testing.T) {
	machineID, identity := newTestIdentity(t)
	remote := newFakeRemote(t, machineID, identity, http.StatusNoContent)

	peer := RemotePeer{BaseURL: remote.server.URL, MachineID: machineID, IdentityKey: identity.PublicKey(), Token: "dead-token"}
	var got []any
	if err := FetchRemoteJSON(context.Background(), remote.server.Client(), peer, "/api/sessions", 1<<20, &got); err == nil {
		t.Fatal("fetch succeeded with a refused credential")
	}
}
