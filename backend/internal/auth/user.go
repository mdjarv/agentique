package auth

import (
	"encoding/base64"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/allbin/agentique/backend/internal/store"
)

// User implements the webauthn.User interface backed by store types.
type User struct {
	store.User
	Credentials []webauthn.Credential
}

func (u *User) WebAuthnID() []byte {
	return []byte(u.ID)
}

func (u *User) WebAuthnName() string {
	return u.DisplayName
}

func (u *User) WebAuthnDisplayName() string {
	return u.DisplayName
}

func (u *User) WebAuthnCredentials() []webauthn.Credential {
	return u.Credentials
}

// credentialFromStore converts a store.WebauthnCredential to a webauthn.Credential.
func credentialFromStore(c store.WebauthnCredential) webauthn.Credential {
	id, _ := base64.RawURLEncoding.DecodeString(c.ID)

	var transports []protocol.AuthenticatorTransport
	if c.Transport != "" {
		for _, t := range strings.Split(c.Transport, ",") {
			transports = append(transports, protocol.AuthenticatorTransport(t))
		}
	}

	return webauthn.Credential{
		ID:              id,
		PublicKey:       c.PublicKey,
		AttestationType: c.AttestationType,
		Transport:       transports,
		Flags: webauthn.CredentialFlags{
			BackupEligible: c.BackupEligible != 0,
			BackupState:    c.BackupState != 0,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:    c.Aaguid,
			SignCount: uint32(c.SignCount),
		},
	}
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// credentialsFromStore converts a slice of store credentials.
func credentialsFromStore(creds []store.WebauthnCredential) []webauthn.Credential {
	out := make([]webauthn.Credential, len(creds))
	for i, c := range creds {
		out[i] = credentialFromStore(c)
	}
	return out
}
