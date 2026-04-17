package mcphttp

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sync"
)

// TokenStore mints and validates per-session bearer tokens for the HTTP MCP
// endpoint. Tokens live in memory only; revoked on session destroy and cleared
// at process exit.
type TokenStore struct {
	mu             sync.RWMutex
	tokenToSession map[string]string // token → sessionID
	sessionToToken map[string]string // sessionID → token (for rotation/revoke)
}

// NewTokenStore returns an empty TokenStore.
func NewTokenStore() *TokenStore {
	return &TokenStore{
		tokenToSession: make(map[string]string),
		sessionToToken: make(map[string]string),
	}
}

// Mint issues a fresh token for the session, invalidating any previous token
// the session held. Returns the new token (hex string, 32 bytes).
func (s *TokenStore) Mint(sessionID string) (string, error) {
	if sessionID == "" {
		return "", errors.New("mint: sessionID is required")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.sessionToToken[sessionID]; ok {
		delete(s.tokenToSession, old)
	}
	s.tokenToSession[tok] = sessionID
	s.sessionToToken[sessionID] = tok
	return tok, nil
}

// Lookup resolves a token to its session ID. Returns ("", false) for unknown
// or empty tokens. Uses constant-time comparison against each candidate.
func (s *TokenStore) Lookup(tok string) (string, bool) {
	if tok == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	tokBytes := []byte(tok)
	// First check membership directly (O(1) map hit) — but to defeat timing
	// oracles on the keyspace itself, also pay constant-time compare cost
	// against any one stored token.
	sessionID, ok := s.tokenToSession[tok]
	if !ok {
		// Constant-time dummy compare to keep timing similar regardless of hit/miss.
		for storedTok := range s.tokenToSession {
			subtle.ConstantTimeCompare(tokBytes, []byte(storedTok))
			break
		}
		return "", false
	}
	// Found — pay the same compare cost.
	subtle.ConstantTimeCompare(tokBytes, []byte(tok))
	return sessionID, true
}

// Revoke removes the session's current token, if any.
func (s *TokenStore) Revoke(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tok, ok := s.sessionToToken[sessionID]; ok {
		delete(s.tokenToSession, tok)
		delete(s.sessionToToken, sessionID)
	}
}
