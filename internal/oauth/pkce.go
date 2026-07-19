// Package oauth implements the interactive OAuth 2.1 browser login flow against
// VibeXP's embedded authorization server: RFC 8414 discovery, RFC 7591 dynamic
// client registration, authorization-code + PKCE (S256) over a loopback
// callback, RFC 8707 resource-indicated token exchange, and transparent
// rotating refresh serialized across processes with a file lock.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCEMethod is the only code challenge method the server accepts.
const PKCEMethod = "S256"

// PKCE holds a generated verifier/challenge pair.
type PKCE struct {
	Verifier  string
	Challenge string
	Method    string
}

// GeneratePKCE creates a 32-byte high-entropy verifier and its S256 challenge.
func GeneratePKCE() (PKCE, error) {
	verifier, err := randomURLSafe(32)
	if err != nil {
		return PKCE{}, fmt.Errorf("generate PKCE verifier: %w", err)
	}
	sum := sha256.Sum256([]byte(verifier))
	return PKCE{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
		Method:    PKCEMethod,
	}, nil
}

// GenerateState creates a random anti-CSRF state nonce.
func GenerateState() (string, error) {
	s, err := randomURLSafe(16)
	if err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return s, nil
}

// randomURLSafe returns n random bytes encoded as unpadded base64url.
func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
