package cred

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Path: filepath.Join(t.TempDir(), "credentials.json")}
}

func TestGetMissingReturnsNil(t *testing.T) {
	s := tempStore(t)
	e, err := s.Get("nope")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if e != nil {
		t.Errorf("expected nil entry, got %+v", e)
	}
}

func TestSaveGetRoundTripAndPerms(t *testing.T) {
	s := tempStore(t)
	if err := s.Save("dev", Entry{Type: TypeAPIKey, APIKey: "vxk_secret_value_123456"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(s.Path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("credentials perms = %o, want 600", perm)
		}
	}

	e, err := s.Get("dev")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if e == nil || e.Type != TypeAPIKey || e.APIKey != "vxk_secret_value_123456" {
		t.Errorf("round-trip mismatch: %+v", e)
	}
}

func TestDeleteOnlyTargetContext(t *testing.T) {
	s := tempStore(t)
	_ = s.Save("dev", Entry{Type: TypeAPIKey, APIKey: "dev_key_abcdef"})
	_ = s.Save("prod", Entry{Type: TypeAPIKey, APIKey: "prod_key_abcdef"})

	removed, err := s.Delete("dev")
	if err != nil || !removed {
		t.Fatalf("Delete dev: removed=%v err=%v", removed, err)
	}
	if e, _ := s.Get("dev"); e != nil {
		t.Error("dev entry should be gone")
	}
	if e, _ := s.Get("prod"); e == nil {
		t.Error("prod entry must be untouched")
	}

	// Deleting an absent entry is a no-op, not an error.
	removed, err = s.Delete("ghost")
	if err != nil || removed {
		t.Errorf("Delete ghost: removed=%v err=%v, want false/nil", removed, err)
	}
}

func TestFingerprint(t *testing.T) {
	cases := map[string]string{
		"vxk_abcdefgh1234": "vxk_…1234",
		"short":            "****",
		"12345678":         "****",
		"123456789":        "1234…6789",
	}
	for in, want := range cases {
		if got := Fingerprint(in); got != want {
			t.Errorf("Fingerprint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolvePrecedence(t *testing.T) {
	s := tempStore(t)
	_ = s.Save("dev", Entry{Type: TypeAPIKey, APIKey: "stored_key_abcdef"})

	// env wins over stored.
	env := func(k string) string {
		if k == EnvAPIKey {
			return "env_key_123456"
		}
		return ""
	}
	r, err := s.Resolve("dev", env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != SourceEnv || r.Bearer != "env_key_123456" {
		t.Errorf("expected env source, got %+v", r)
	}

	// no env -> stored.
	r, _ = s.Resolve("dev", func(string) string { return "" })
	if r.Source != SourceStored || r.Bearer != "stored_key_abcdef" {
		t.Errorf("expected stored source, got %+v", r)
	}

	// no env, no stored -> none.
	r, _ = s.Resolve("prod", func(string) string { return "" })
	if r.Source != SourceNone || r.Bearer != "" {
		t.Errorf("expected none, got %+v", r)
	}
}

func TestResolveOAuthEntryUsesAccessToken(t *testing.T) {
	s := tempStore(t)
	_ = s.Save("dev", Entry{Type: TypeOAuth, AccessToken: "access_token_abcdef", RefreshToken: "refresh_xyz"})
	r, _ := s.Resolve("dev", func(string) string { return "" })
	if r.Bearer != "access_token_abcdef" || r.Type != TypeOAuth {
		t.Errorf("oauth resolve mismatch: %+v", r)
	}
}
