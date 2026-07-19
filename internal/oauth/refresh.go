package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/vibexp/cli/internal/cred"
)

// ErrReauthRequired signals that the stored OAuth session cannot be renewed
// (refresh token expired, revoked, or reuse-detected) and the user must run
// `vibexp auth login` again. Commands map this to exit code 4.
var ErrReauthRequired = errors.New("session expired; run 'vibexp auth login' to sign in again")

// defaultLeeway refreshes slightly before the real expiry to avoid races with
// server-side clock skew and in-flight requests.
const defaultLeeway = 60 * time.Second

// Refresher produces valid access tokens for an OAuth context, transparently
// refreshing expired tokens. Refresh is serialized across concurrent processes
// with a file lock and persists rotated refresh tokens atomically before
// releasing the lock, so exactly one process refreshes.
type Refresher struct {
	HTTPClient    *http.Client
	Store         *cred.Store
	ContextName   string
	TokenEndpoint string
	ClientID      string
	Resource      string
	// LockPath defaults to credentials.lock beside the credential store.
	LockPath string
	// Leeway defaults to defaultLeeway.
	Leeway time.Duration
	// Now defaults to time.Now (injectable for tests).
	Now func() time.Time
}

func (r *Refresher) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Refresher) leeway() time.Duration {
	if r.Leeway > 0 {
		return r.Leeway
	}
	return defaultLeeway
}

func (r *Refresher) lockPath() string {
	if r.LockPath != "" {
		return r.LockPath
	}
	return filepath.Join(filepath.Dir(r.Store.Path), "credentials.lock")
}

// entryToken reads the stored OAuth entry as a Token.
func (r *Refresher) entryToken() (*cred.Entry, *Token, error) {
	entry, err := r.Store.Get(r.ContextName)
	if err != nil {
		return nil, nil, err
	}
	if entry == nil || entry.Type != cred.TypeOAuth {
		return nil, nil, fmt.Errorf("no OAuth session for context %q", r.ContextName)
	}
	return entry, &Token{
		AccessToken:  entry.AccessToken,
		RefreshToken: entry.RefreshToken,
		Expiry:       entry.ExpiresAt,
	}, nil
}

// AccessToken returns a valid access token, refreshing under a cross-process
// lock if the stored token is expired.
func (r *Refresher) AccessToken(ctx context.Context) (string, error) {
	_, tok, err := r.entryToken()
	if err != nil {
		return "", err
	}
	if tok.Valid(r.now(), r.leeway()) {
		return tok.AccessToken, nil
	}

	lock, err := acquireLock(r.lockPath())
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Unlock() }()

	// Re-read under the lock: another process may have refreshed while we waited.
	entry, tok, err := r.entryToken()
	if err != nil {
		return "", err
	}
	if tok.Valid(r.now(), r.leeway()) {
		return tok.AccessToken, nil
	}
	if tok.RefreshToken == "" {
		return "", ErrReauthRequired
	}

	newTok, err := Refresh(ctx, r.HTTPClient, r.TokenEndpoint, r.ClientID, tok.RefreshToken, r.Resource)
	if errors.Is(err, ErrInvalidGrant) {
		_, _ = r.Store.Delete(r.ContextName) // wipe the unusable session
		return "", ErrReauthRequired
	}
	if err != nil {
		return "", err
	}

	// Persist rotated tokens atomically BEFORE releasing the lock.
	entry.AccessToken = newTok.AccessToken
	if newTok.RefreshToken != "" {
		entry.RefreshToken = newTok.RefreshToken // honor rotation
	}
	entry.ExpiresAt = newTok.Expiry
	if err := r.Store.Save(r.ContextName, *entry); err != nil {
		return "", err
	}
	return newTok.AccessToken, nil
}
