// Package update implements the gh-style update system: a cached, non-blocking
// GitHub Releases version check that prints a single stderr notice when a newer
// release exists, and an explicit, provenance-aware `vibexp update` that
// checksum-verifies and atomically self-replaces the binary. It never
// auto-applies and never blocks or fails the wrapped command.
package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State is the persisted update-check cache (~/.vibexp/state.json). It exists
// only to rate-limit the network check and remember the last seen release.
type State struct {
	// LastCheck is when the GitHub Releases API was last queried.
	LastCheck time.Time `json:"last_check"`
	// LatestSeen is the most recent release tag observed (e.g. "v1.2.3").
	LatestSeen string `json:"latest_seen,omitempty"`
	// ETag is the release response's ETag, sent as If-None-Match to get cheap
	// 304s that don't count against the unauthenticated rate limit body.
	ETag string `json:"etag,omitempty"`
}

// DefaultStatePath returns ~/.vibexp/state.json.
func DefaultStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".vibexp", "state.json"), nil
}

// LoadState reads the state file; a missing or unreadable file yields a zero
// State (the check simply treats it as "never checked").
func LoadState(path string) State {
	var st State
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}
	}
	return st
}

// SaveState atomically writes the state file (best effort — a failure here must
// never surface to the user, since the check is advisory).
func SaveState(path string, st State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	out, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
