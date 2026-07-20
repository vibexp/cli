// Package cred manages the per-context credential store
// (~/.vibexp/credentials.json, 0600). It is deliberately separate from the
// config store so credentials never mingle with non-secret settings. The
// on-disk schema already accommodates OAuth token entries (issue #5).
package cred

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credential entry types.
const (
	TypeAPIKey = "api_key"
	TypeOAuth  = "oauth"
)

// Entry is one context's stored credential. API-key entries set Type=api_key
// and APIKey; OAuth entries (issue #5) set Type=oauth and the token fields.
type Entry struct {
	Type         string    `json:"type"`
	APIKey       string    `json:"api_key,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	// Scopes are the scopes declared when ClientID was registered (RFC 7591
	// scope). Browser login reuses a stored client only when its scopes cover
	// the scopes it needs to request; older entries have none and re-register.
	Scopes []string `json:"scopes,omitempty"`
}

// file is the on-disk document: a map of context name -> entry.
type file struct {
	Credentials map[string]Entry `json:"credentials"`
}

// Store reads and writes the credential file at Path.
type Store struct {
	Path string
}

// DefaultPath returns ~/.vibexp/credentials.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".vibexp", "credentials.json"), nil
}

// DefaultStore returns a Store rooted at the default credentials path.
func DefaultStore() (*Store, error) {
	p, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return &Store{Path: p}, nil
}

// load reads the credential file; a missing file is an empty document.
func (s *Store) load() (*file, error) {
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return &file{Credentials: map[string]Entry{}}, nil
		}
		return nil, fmt.Errorf("read credentials %s: %w", s.Path, err)
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse credentials %s: %w", s.Path, err)
	}
	if f.Credentials == nil {
		f.Credentials = map[string]Entry{}
	}
	return &f, nil
}

// Get returns the entry for a context, or nil if none is stored.
func (s *Store) Get(contextName string) (*Entry, error) {
	f, err := s.load()
	if err != nil {
		return nil, err
	}
	e, ok := f.Credentials[contextName]
	if !ok {
		return nil, nil
	}
	return &e, nil
}

// Save persists (inserting or replacing) the entry for a context.
func (s *Store) Save(contextName string, e Entry) error {
	f, err := s.load()
	if err != nil {
		return err
	}
	f.Credentials[contextName] = e
	return s.write(f)
}

// Delete removes a context's entry. Removing an absent entry is a no-op.
func (s *Store) Delete(contextName string) (bool, error) {
	f, err := s.load()
	if err != nil {
		return false, err
	}
	if _, ok := f.Credentials[contextName]; !ok {
		return false, nil
	}
	delete(f.Credentials, contextName)
	return true, s.write(f)
}

// write serializes the document atomically (temp file + rename) with 0600
// perms and a 0700 parent directory.
func (s *Store) write(f *file) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create credentials dir %s: %w", dir, err)
	}
	out, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp credentials: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp credentials: %w", err)
	}
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp credentials: %w", err)
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return fmt.Errorf("rename credentials into place: %w", err)
	}
	return nil
}

// Fingerprint returns a non-secret display form of a key: the first 4 and last
// 4 characters (e.g. "vxk_…1234"). Short keys collapse to a fixed mask.
func Fingerprint(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "…" + key[len(key)-4:]
}
