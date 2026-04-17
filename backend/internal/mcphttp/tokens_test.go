package mcphttp

import (
	"strings"
	"testing"
)

func TestMintProducesUniqueTokens(t *testing.T) {
	ts := NewTokenStore()

	a, err := ts.Mint("sess-A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ts.Mint("sess-B")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two mints should produce different tokens")
	}
	if len(a) < 32 {
		t.Errorf("token too short: %d chars", len(a))
	}
}

func TestMintSameSessionRotatesToken(t *testing.T) {
	ts := NewTokenStore()
	a, _ := ts.Mint("sess-A")
	b, _ := ts.Mint("sess-A")

	if a == b {
		t.Error("re-mint should rotate token")
	}
	// Old token must no longer resolve.
	if _, ok := ts.Lookup(a); ok {
		t.Error("old token should be invalidated after rotation")
	}
	if got, ok := ts.Lookup(b); !ok || got != "sess-A" {
		t.Errorf("new token should resolve to sess-A, got (%q, %v)", got, ok)
	}
}

func TestLookupKnownToken(t *testing.T) {
	ts := NewTokenStore()
	tok, _ := ts.Mint("sess-A")

	got, ok := ts.Lookup(tok)
	if !ok {
		t.Fatal("known token should be valid")
	}
	if got != "sess-A" {
		t.Errorf("want sess-A, got %s", got)
	}
}

func TestLookupUnknownToken(t *testing.T) {
	ts := NewTokenStore()
	_, _ = ts.Mint("sess-A")

	if _, ok := ts.Lookup("not-a-real-token"); ok {
		t.Error("unknown token must not be valid")
	}
	if _, ok := ts.Lookup(""); ok {
		t.Error("empty token must not be valid")
	}
}

func TestRevokeBySessionID(t *testing.T) {
	ts := NewTokenStore()
	tok, _ := ts.Mint("sess-A")

	ts.Revoke("sess-A")

	if _, ok := ts.Lookup(tok); ok {
		t.Error("token must not resolve after revoke")
	}
}

func TestRevokeUnknownIsNoop(t *testing.T) {
	ts := NewTokenStore()
	ts.Revoke("never-existed") // must not panic
}

func TestMintRejectsEmptySessionID(t *testing.T) {
	ts := NewTokenStore()
	_, err := ts.Mint("")
	if err == nil {
		t.Error("want error for empty session id")
	}
}

func TestTokenIsHexLike(t *testing.T) {
	ts := NewTokenStore()
	tok, _ := ts.Mint("sess-A")
	for _, c := range tok {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isHex {
			t.Fatalf("token has non-hex char %q in %s", c, tok)
		}
	}
	// Sanity: must look like 32+ hex chars.
	if !strings.ContainsAny(tok, "0123456789abcdef") {
		t.Error("token should contain hex characters")
	}
}
