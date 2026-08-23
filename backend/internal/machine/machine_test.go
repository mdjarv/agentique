package machine

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestLoadOrCreateIDStable(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreateID(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("id %q is not a UUID: %v", first, err)
	}

	second, err := LoadOrCreateID(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if second != first {
		t.Fatalf("id changed across calls: %q != %q", second, first)
	}
}

func TestLoadOrCreateIDRegeneratesCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, idFileName), []byte("not-a-uuid\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	id, err := LoadOrCreateID(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("regenerated id %q is not a UUID: %v", id, err)
	}
}

func TestLabelOverrideWins(t *testing.T) {
	if got := Label("  my-box  "); got != "my-box" {
		t.Fatalf("Label(override) = %q, want %q", got, "my-box")
	}
	if got := Label(""); got == "" {
		t.Fatal("Label must never be empty")
	}
}

func TestSigningIdentityIsStableAndVerifiable(t *testing.T) {
	dir := t.TempDir()
	machineID := uuid.New().String()
	identity, err := LoadOrCreateSigningIdentity(dir, machineID)
	if err != nil {
		t.Fatalf("create signing identity: %v", err)
	}

	nonce := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	proof, err := identity.SignChallenge(nonce)
	if err != nil {
		t.Fatalf("sign challenge: %v", err)
	}
	if err := VerifyChallenge(identity.PublicKey(), machineID, nonce, proof); err != nil {
		t.Fatalf("verify challenge: %v", err)
	}
	if err := VerifyChallenge(identity.PublicKey(), uuid.New().String(), nonce, proof); err == nil {
		t.Fatal("proof verified for a different machine id")
	}

	reloaded, err := LoadOrCreateSigningIdentity(dir, machineID)
	if err != nil {
		t.Fatalf("reload signing identity: %v", err)
	}
	if reloaded.PublicKey() != identity.PublicKey() {
		t.Fatal("public key changed across reload")
	}
	info, err := os.Stat(filepath.Join(dir, signingKeyFileName))
	if err != nil {
		t.Fatalf("stat signing key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("signing key mode = %o, want 600", info.Mode().Perm())
	}
}
