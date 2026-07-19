// Package config manages named contexts (kubectl-style) persisted to
// ~/.vibexp/config.yaml. It also resolves the effective runtime settings using
// the precedence: flag > env > active context.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	yamlv3 "gopkg.in/yaml.v3"
)

// Context is a single named target: a base URL plus optional default team and
// project. Credentials are NOT stored here — they live in credentials.json.
type Context struct {
	Name           string `koanf:"name" yaml:"name"`
	BaseURL        string `koanf:"base_url" yaml:"base_url"`
	DefaultTeam    string `koanf:"team" yaml:"team,omitempty"`
	DefaultProject string `koanf:"project" yaml:"project,omitempty"`
}

// File is the on-disk config document.
type File struct {
	CurrentContext string    `koanf:"current_context" yaml:"current_context"`
	Contexts       []Context `koanf:"contexts" yaml:"contexts"`
}

// Find returns the context with the given name, or nil if absent.
func (f *File) Find(name string) *Context {
	for i := range f.Contexts {
		if f.Contexts[i].Name == name {
			return &f.Contexts[i]
		}
	}
	return nil
}

// Upsert inserts or replaces a context by name.
func (f *File) Upsert(c Context) {
	if existing := f.Find(c.Name); existing != nil {
		*existing = c
		return
	}
	f.Contexts = append(f.Contexts, c)
}

// Store reads and writes the config file at Path.
type Store struct {
	Path string
}

// DefaultPath returns ~/.vibexp/config.yaml.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".vibexp", "config.yaml"), nil
}

// DefaultStore returns a Store rooted at the default config path.
func DefaultStore() (*Store, error) {
	p, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return &Store{Path: p}, nil
}

// Load reads and parses the config file with koanf. A missing file yields an
// empty File (not an error) so a fresh install works out of the box.
func (s *Store) Load() (*File, error) {
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{}, nil
		}
		return nil, fmt.Errorf("read config %s: %w", s.Path, err)
	}
	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider(raw), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", s.Path, err)
	}
	var f File
	if err := k.Unmarshal("", &f); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", s.Path, err)
	}
	return &f, nil
}

// Save writes the config atomically (temp file + rename) with 0600 perms and a
// 0700 parent directory.
func (s *Store) Save(f *File) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	out, err := yamlv3.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return fmt.Errorf("rename config into place: %w", err)
	}
	return nil
}
