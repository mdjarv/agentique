package machine

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
)

const (
	signingKeyFileName = "machine-identity-key.pem"
	challengeDomain    = "agentique-machine-proof-v1\n"
	challengeNonceSize = 32
)

// SigningIdentity is the machine's persistent cryptographic identity. The
// public key is pinned by paired clients; the private key never leaves the
// data directory.
type SigningIdentity struct {
	machineID  string
	privateKey *ecdsa.PrivateKey
	publicKey  string
}

// LoadOrCreateSigningIdentity reads the P-256 key from dataDir or creates it
// with owner-only permissions. A corrupt existing key is an error because
// silently replacing it would strand every client that pinned the old key.
func LoadOrCreateSigningIdentity(dataDir, machineID string) (*SigningIdentity, error) {
	path := filepath.Join(dataDir, signingKeyFileName)
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("secure machine identity key: %w", err)
		}
		return parseSigningIdentity(raw, machineID)
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read machine identity key: %w", err)
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate machine identity key: %w", err)
	}
	der, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("encode machine identity key: %w", err)
	}
	raw = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, fmt.Errorf("persist machine identity key: %w", err)
	}
	return newSigningIdentity(machineID, privateKey)
}

func parseSigningIdentity(raw []byte, machineID string) (*SigningIdentity, error) {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "EC PRIVATE KEY" || len(rest) != 0 {
		return nil, errors.New("machine identity key is not a single EC private key PEM block")
	}
	privateKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse machine identity key: %w", err)
	}
	if privateKey.Curve != elliptic.P256() {
		return nil, errors.New("machine identity key must use P-256")
	}
	return newSigningIdentity(machineID, privateKey)
}

func newSigningIdentity(machineID string, privateKey *ecdsa.PrivateKey) (*SigningIdentity, error) {
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("encode machine identity public key: %w", err)
	}
	return &SigningIdentity{
		machineID:  machineID,
		privateKey: privateKey,
		publicKey:  base64.RawURLEncoding.EncodeToString(publicDER),
	}, nil
}

// PublicKey returns the base64url-encoded SubjectPublicKeyInfo DER pinned by
// clients during pairing.
func (i *SigningIdentity) PublicKey() string {
	if i == nil {
		return ""
	}
	return i.publicKey
}

// SignChallenge signs a fresh 32-byte base64url nonce for this machine.
func (i *SigningIdentity) SignChallenge(nonce string) (string, error) {
	if i == nil || i.privateKey == nil {
		return "", errors.New("machine signing identity is unavailable")
	}
	if err := validateChallengeNonce(nonce); err != nil {
		return "", err
	}
	digest := challengeDigest(i.machineID, nonce)
	r, s, err := ecdsa.Sign(rand.Reader, i.privateKey, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign machine challenge: %w", err)
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return base64.RawURLEncoding.EncodeToString(signature), nil
}

// VerifyChallenge verifies a proof against a pinned public key and machine id.
func VerifyChallenge(publicKey, machineID, nonce, proof string) error {
	if err := validateChallengeNonce(nonce); err != nil {
		return err
	}
	key, err := parseIdentityPublicKey(publicKey)
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(proof)
	if err != nil || len(signature) != 64 {
		return errors.New("decode machine identity proof")
	}
	digest := challengeDigest(machineID, nonce)
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(key, digest[:], r, s) {
		return errors.New("machine identity proof is invalid")
	}
	return nil
}

// ValidateIdentityPublicKey checks the encoded key format accepted by paired
// clients and the proof verifier.
func ValidateIdentityPublicKey(publicKey string) error {
	_, err := parseIdentityPublicKey(publicKey)
	return err
}

func parseIdentityPublicKey(publicKey string) (*ecdsa.PublicKey, error) {
	publicDER, err := base64.RawURLEncoding.DecodeString(publicKey)
	if err != nil {
		return nil, errors.New("decode machine identity public key")
	}
	parsed, err := x509.ParsePKIXPublicKey(publicDER)
	if err != nil {
		return nil, errors.New("parse machine identity public key")
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("machine identity public key must use P-256")
	}
	return key, nil
}

func validateChallengeNonce(nonce string) error {
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil || len(raw) != challengeNonceSize {
		return fmt.Errorf("challenge nonce must be %d base64url bytes", challengeNonceSize)
	}
	return nil
}

func challengeDigest(machineID, nonce string) [sha256.Size]byte {
	return sha256.Sum256([]byte(challengeDomain + machineID + "\n" + nonce))
}
