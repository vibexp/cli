package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Path: filepath.Join(t.TempDir(), "config.yaml")}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "does-not-exist.yaml")}
	f, err := s.Load()
	if err != nil {
		t.Fatalf("Load of missing file: %v", err)
	}
	if f.CurrentContext != "" || len(f.Contexts) != 0 {
		t.Fatalf("expected empty File, got %+v", f)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := tempStore(t)
	in := &File{
		CurrentContext: "staging",
		Contexts: []Context{
			{Name: "staging", BaseURL: "https://staging.example", DefaultTeam: "team-a"},
			{Name: "prod", BaseURL: "https://prod.example", DefaultProject: "proj-b"},
		},
	}
	if err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File perms must be 0600 (unix only — Windows ACLs don't map to mode bits).
	if runtime.GOOS != "windows" {
		info, err := os.Stat(s.Path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("config perms = %o, want 600", perm)
		}
	}

	out, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.CurrentContext != "staging" {
		t.Errorf("CurrentContext = %q, want staging", out.CurrentContext)
	}
	if len(out.Contexts) != 2 {
		t.Fatalf("got %d contexts, want 2", len(out.Contexts))
	}
	if c := out.Find("staging"); c == nil || c.BaseURL != "https://staging.example" || c.DefaultTeam != "team-a" {
		t.Errorf("staging context round-trip wrong: %+v", c)
	}
}

func TestUpsertReplacesByName(t *testing.T) {
	f := &File{}
	f.Upsert(Context{Name: "x", BaseURL: "a"})
	f.Upsert(Context{Name: "x", BaseURL: "b"})
	if len(f.Contexts) != 1 {
		t.Fatalf("expected 1 context after upsert, got %d", len(f.Contexts))
	}
	if f.Contexts[0].BaseURL != "b" {
		t.Errorf("BaseURL = %q, want b", f.Contexts[0].BaseURL)
	}
}

func TestResolvePrecedence(t *testing.T) {
	f := &File{
		CurrentContext: "ctx",
		Contexts: []Context{{
			Name: "ctx", BaseURL: "https://ctx", DefaultTeam: "ctx-team", DefaultProject: "ctx-proj",
		}},
	}
	env := func(k string) string {
		return map[string]string{
			EnvTeam:    "env-team",
			EnvBaseURL: "https://env",
		}[k]
	}

	// flag > env > context.
	rt := f.Resolve(Overrides{Team: "flag-team"}, env)
	if rt.Team != "flag-team" {
		t.Errorf("Team = %q, want flag-team (flag wins)", rt.Team)
	}
	// env beats context when no flag.
	if rt.BaseURL != "https://env" {
		t.Errorf("BaseURL = %q, want https://env (env wins)", rt.BaseURL)
	}
	// context used when neither flag nor env set.
	if rt.Project != "ctx-proj" {
		t.Errorf("Project = %q, want ctx-proj (context fallback)", rt.Project)
	}
	if rt.ContextName != "ctx" {
		t.Errorf("ContextName = %q, want ctx", rt.ContextName)
	}
}

func TestResolveContextSelectionPrecedence(t *testing.T) {
	f := &File{
		CurrentContext: "default",
		Contexts: []Context{
			{Name: "default", BaseURL: "https://default"},
			{Name: "other", BaseURL: "https://other"},
		},
	}
	env := func(k string) string {
		if k == EnvContext {
			return "other"
		}
		return ""
	}
	// --context flag beats VIBEXP_CONTEXT env.
	rt := f.Resolve(Overrides{Context: "default"}, env)
	if rt.ContextName != "default" || rt.BaseURL != "https://default" {
		t.Errorf("flag context should win: got %+v", rt)
	}
	// env beats CurrentContext.
	rt = f.Resolve(Overrides{}, env)
	if rt.ContextName != "other" || rt.BaseURL != "https://other" {
		t.Errorf("env context should win over current: got %+v", rt)
	}
}
