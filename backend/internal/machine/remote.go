package machine

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
)

const maxIdentityResponseBytes = 64 << 10

// RevokeRemoteBearer proves the pinned server identity, then revokes the
// bearer on that server. The credential is never sent before the proof passes.
func RevokeRemoteBearer(ctx context.Context, client *http.Client, baseURL, machineID, identityKey, token string) error {
	if client == nil {
		return errors.New("remote machine HTTP client is unavailable")
	}
	if identityKey == "" || token == "" {
		return errors.New("remote machine must be re-paired before removal")
	}
	if err := proveRemoteIdentity(ctx, client, baseURL, machineID, identityKey); err != nil {
		return err
	}

	revoke, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL+"/api/auth/session", nil)
	if err != nil {
		return fmt.Errorf("create remote revoke request: %w", err)
	}
	revoke.Header.Set("Authorization", "Bearer "+token)
	revokeResp, err := client.Do(revoke)
	if err != nil {
		return fmt.Errorf("revoke remote bearer: %w", err)
	}
	defer revokeResp.Body.Close()
	// A refused credential is already revoked: the remote does not honour it,
	// which is the state this call exists to reach. Treating that as a failure
	// stranded the entry — a machine whose bearer died could be neither used nor
	// removed. The identity proof above still ran, so this is not a way to
	// delete a pairing without proving who is answering.
	if revokeResp.StatusCode == http.StatusUnauthorized || revokeResp.StatusCode == http.StatusForbidden {
		return nil
	}
	if revokeResp.StatusCode != http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(revokeResp.Body, maxIdentityResponseBytes))
		return fmt.Errorf("revoke remote bearer: status %d", revokeResp.StatusCode)
	}
	return nil
}

// RemotePeer is what a server needs to read from a paired machine as that
// machine's client: where it is, who it must prove to be, and the bearer it
// accepts. It is a catalog row's outbound half and nothing more.
type RemotePeer struct {
	BaseURL     string
	MachineID   string
	IdentityKey string
	Token       string
}

// FetchRemoteJSON reads one authenticated GET endpoint from a paired machine
// into dst, after proving the machine is the one the catalog pinned.
//
// The same order the browser's machineFetch keeps and [RevokeRemoteBearer]
// keeps: the signed challenge first, the bearer only once it verifies, so a
// host answering at a stale or hijacked address never receives the credential.
// maxBytes bounds the body, because the answer comes from another machine.
func FetchRemoteJSON(ctx context.Context, client *http.Client, peer RemotePeer, path string, maxBytes int64, dst any) error {
	return DoRemoteJSON(ctx, client, peer, http.MethodGet, path, nil, maxBytes, dst)
}

// RemoteStatusError is a paired machine answering with a status other than
// 200. Body is the bounded response, so a caller that knows the route's error
// shape can decode a refusal from it.
type RemoteStatusError struct {
	Status int
	Body   []byte
}

func (e *RemoteStatusError) Error() string { return fmt.Sprintf("status %d", e.Status) }

// UnansweredError is a request that reached the wire and got no answer back: a
// timeout, a reset, a yes whose body did not arrive.
//
// Whether the remote acted on it is unknown, which for a request that creates
// or sends something is a different fact from a failure — the remote may have
// done it. A failure before the request left (identity proof, a dial that was
// refused) is never this.
type UnansweredError struct {
	Path string
	Err  error
}

func (e *UnansweredError) Error() string {
	return fmt.Sprintf("request %s got no answer: %v", e.Path, e.Err)
}

func (e *UnansweredError) Unwrap() error { return e.Err }

// DoRemoteJSON is [FetchRemoteJSON] for any method: identity proof first, then
// the request with the bearer and body (JSON-encoded when non-nil), decoding a
// 200 into dst. Any other status is a [*RemoteStatusError].
func DoRemoteJSON(ctx context.Context, client *http.Client, peer RemotePeer, method, path string, body any, maxBytes int64, dst any) error {
	if client == nil {
		return errors.New("remote machine HTTP client is unavailable")
	}
	if peer.IdentityKey == "" || peer.Token == "" {
		return errors.New("remote machine must be re-paired before it can be read")
	}
	if err := proveRemoteIdentity(ctx, client, peer.BaseURL, peer.MachineID, peer.IdentityKey); err != nil {
		return err
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request to %s: %w", path, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, peer.BaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("create remote request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+peer.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Whether the request left is the fact a caller needs when no answer comes
	// back, and the error cannot say it: a timeout reads the same before the
	// connection opened and after the remote had the whole body.
	var wrote atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		WroteHeaders: func() { wrote.Store(true) },
	}))
	resp, err := client.Do(req)
	if err != nil {
		if wrote.Load() {
			return &UnansweredError{Path: path, Err: err}
		}
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		if resp.StatusCode == http.StatusOK {
			// It answered yes and the answer did not arrive: whatever it did, it did.
			return &UnansweredError{Path: path, Err: fmt.Errorf("read: %w", err)}
		}
		return &RemoteStatusError{Status: resp.StatusCode}
	}
	if int64(len(raw)) > maxBytes {
		return fmt.Errorf("read %s: response exceeds %d bytes", path, maxBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return &RemoteStatusError{Status: resp.StatusCode, Body: raw}
	}
	if dst == nil {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// proveRemoteIdentity checks that the server at baseURL holds the signing key
// pinned for machineID, by descriptor and then by a fresh signed challenge.
func proveRemoteIdentity(ctx context.Context, client *http.Client, baseURL, machineID, identityKey string) error {
	var descriptor struct {
		MachineID   string `json:"machineId"`
		IdentityKey string `json:"identityKey"`
	}
	if err := getBoundedJSON(ctx, client, baseURL+"/.well-known/agentique/environment", &descriptor); err != nil {
		return fmt.Errorf("read remote identity: %w", err)
	}
	if descriptor.MachineID != machineID || descriptor.IdentityKey != identityKey {
		return errors.New("remote machine identity does not match the catalog pin")
	}

	nonceBytes := make([]byte, challengeNonceSize)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmt.Errorf("generate remote identity challenge: %w", err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	requestBody, err := json.Marshal(map[string]string{"nonce": nonce})
	if err != nil {
		return fmt.Errorf("encode remote identity challenge: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/auth/identity-proof", bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("create remote identity request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request remote identity proof: %w", err)
	}
	var proof struct {
		MachineID   string `json:"machineId"`
		IdentityKey string `json:"identityKey"`
		Proof       string `json:"proof"`
	}
	if err := decodeBoundedJSONResponse(resp, &proof); err != nil {
		return fmt.Errorf("read remote identity proof: %w", err)
	}
	if proof.MachineID != machineID || proof.IdentityKey != identityKey {
		return errors.New("remote identity proof does not match the catalog pin")
	}
	if err := VerifyChallenge(identityKey, machineID, nonce, proof.Proof); err != nil {
		return fmt.Errorf("verify remote identity proof: %w", err)
	}
	return nil
}

func getBoundedJSON(ctx context.Context, client *http.Client, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return decodeBoundedJSONResponse(resp, dst)
}

func decodeBoundedJSONResponse(resp *http.Response, dst any) error {
	return decodeLimitedJSONResponse(resp, maxIdentityResponseBytes, dst)
}

func decodeLimitedJSONResponse(resp *http.Response, maxBytes int64, dst any) error {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxIdentityResponseBytes))
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > maxBytes {
		return fmt.Errorf("response exceeds %d bytes", maxBytes)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return err
	}
	return nil
}
