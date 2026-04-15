package auth

import (
	"encoding/base64"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/mdjarv/agentique/backend/internal/store"
)

func TestBoolToInt(t *testing.T) {
	if got := boolToInt(true); got != 1 {
		t.Errorf("boolToInt(true) = %d, want 1", got)
	}
	if got := boolToInt(false); got != 0 {
		t.Errorf("boolToInt(false) = %d, want 0", got)
	}
}

func TestCredentialFromStore(t *testing.T) {
	rawID := []byte("test-credential-id")
	encodedID := base64.RawURLEncoding.EncodeToString(rawID)
	aaguid := []byte("test-aaguid-bytes")

	c := store.WebauthnCredential{
		ID:              encodedID,
		PublicKey:       []byte("pubkey"),
		AttestationType: "none",
		Transport:       "usb,nfc",
		BackupEligible:  1,
		BackupState:     0,
		Aaguid:          aaguid,
		SignCount:        42,
	}

	got := credentialFromStore(c)

	if string(got.ID) != string(rawID) {
		t.Errorf("ID = %q, want %q", got.ID, rawID)
	}
	if string(got.PublicKey) != "pubkey" {
		t.Errorf("PublicKey = %q, want %q", got.PublicKey, "pubkey")
	}
	if got.AttestationType != "none" {
		t.Errorf("AttestationType = %q, want %q", got.AttestationType, "none")
	}
	if len(got.Transport) != 2 {
		t.Fatalf("Transport length = %d, want 2", len(got.Transport))
	}
	if got.Transport[0] != protocol.AuthenticatorTransport("usb") {
		t.Errorf("Transport[0] = %q, want %q", got.Transport[0], "usb")
	}
	if got.Transport[1] != protocol.AuthenticatorTransport("nfc") {
		t.Errorf("Transport[1] = %q, want %q", got.Transport[1], "nfc")
	}
	if !got.Flags.BackupEligible {
		t.Error("BackupEligible = false, want true")
	}
	if got.Flags.BackupState {
		t.Error("BackupState = true, want false")
	}
	if string(got.Authenticator.AAGUID) != string(aaguid) {
		t.Errorf("AAGUID = %q, want %q", got.Authenticator.AAGUID, aaguid)
	}
	if got.Authenticator.SignCount != 42 {
		t.Errorf("SignCount = %d, want 42", got.Authenticator.SignCount)
	}
}

func TestCredentialFromStore_EmptyTransport(t *testing.T) {
	c := store.WebauthnCredential{
		ID:        base64.RawURLEncoding.EncodeToString([]byte("id")),
		Transport: "",
	}
	got := credentialFromStore(c)
	if len(got.Transport) != 0 {
		t.Errorf("Transport length = %d, want 0", len(got.Transport))
	}
}

func TestCredentialsFromStore(t *testing.T) {
	creds := []store.WebauthnCredential{
		{ID: base64.RawURLEncoding.EncodeToString([]byte("a")), Transport: "usb"},
		{ID: base64.RawURLEncoding.EncodeToString([]byte("b")), Transport: "nfc"},
	}
	got := credentialsFromStore(creds)
	if len(got) != 2 {
		t.Fatalf("length = %d, want 2", len(got))
	}
	if string(got[0].ID) != "a" {
		t.Errorf("got[0].ID = %q, want %q", got[0].ID, "a")
	}
	if string(got[1].ID) != "b" {
		t.Errorf("got[1].ID = %q, want %q", got[1].ID, "b")
	}
}

func TestUserWebAuthnInterface(t *testing.T) {
	cred := webauthn.Credential{ID: []byte("cred-1")}
	u := &User{
		User:        store.User{ID: "user-123", DisplayName: "Alice"},
		Credentials: []webauthn.Credential{cred},
	}

	if got := string(u.WebAuthnID()); got != "user-123" {
		t.Errorf("WebAuthnID = %q, want %q", got, "user-123")
	}
	if got := u.WebAuthnName(); got != "Alice" {
		t.Errorf("WebAuthnName = %q, want %q", got, "Alice")
	}
	if got := u.WebAuthnDisplayName(); got != "Alice" {
		t.Errorf("WebAuthnDisplayName = %q, want %q", got, "Alice")
	}
	if got := u.WebAuthnCredentials(); len(got) != 1 || string(got[0].ID) != "cred-1" {
		t.Errorf("WebAuthnCredentials unexpected: %v", got)
	}
}
