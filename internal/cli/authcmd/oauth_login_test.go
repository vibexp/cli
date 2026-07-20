package authcmd

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vibexp/cli/internal/cred"
)

func TestResolveScopeOverride(t *testing.T) {
	tests := []struct {
		name string
		flag []string
		env  map[string]string
		want []string
	}{
		{
			name: "no flag, no env yields nil",
			want: nil,
		},
		{
			name: "flag wins over env",
			flag: []string{"api"},
			env:  map[string]string{"VIBEXP_OAUTH_SCOPE": "mcp"},
			want: []string{"api"},
		},
		{
			name: "env used when flag empty",
			env:  map[string]string{"VIBEXP_OAUTH_SCOPE": "openid"},
			want: []string{"openid"},
		},
		{
			name: "env split on spaces and commas",
			env:  map[string]string{"VIBEXP_OAUTH_SCOPE": "openid, profile  api"},
			want: []string{"openid", "profile", "api"},
		},
		{
			name: "blank env yields nil",
			env:  map[string]string{"VIBEXP_OAUTH_SCOPE": "   "},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(k string) string { return tc.env[k] }
			got := resolveScopeOverride(tc.flag, getenv)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("resolveScopeOverride(%v, env=%v) = %v, want %v",
					tc.flag, tc.env, got, tc.want)
			}
		})
	}
}

func TestResolveScopeOverrideNilGetenv(t *testing.T) {
	if got := resolveScopeOverride(nil, nil); got != nil {
		t.Errorf("resolveScopeOverride(nil, nil) = %v, want nil", got)
	}
}

func TestScopesCovered(t *testing.T) {
	tests := []struct {
		name string
		have []string
		want []string
		ok   bool
	}{
		{name: "empty want is always covered", have: nil, want: nil, ok: true},
		{name: "empty want covered even with no have", have: nil, want: []string{}, ok: true},
		{name: "exact match", have: []string{"mcp"}, want: []string{"mcp"}, ok: true},
		{name: "superset covers", have: []string{"mcp", "openid"}, want: []string{"mcp"}, ok: true},
		{name: "missing scope not covered", have: []string{"openid"}, want: []string{"mcp"}, ok: false},
		{name: "legacy entry (no scopes) does not cover a needed scope", have: nil, want: []string{"mcp"}, ok: false},
		{name: "partial coverage is not coverage", have: []string{"mcp"}, want: []string{"mcp", "openid"}, ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scopesCovered(tc.have, tc.want); got != tc.ok {
				t.Errorf("scopesCovered(%v, %v) = %v, want %v", tc.have, tc.want, got, tc.ok)
			}
		})
	}
}

func TestReusableClientID(t *testing.T) {
	const ctxName = "default"
	newStore := func(t *testing.T, e *cred.Entry) *cred.Store {
		t.Helper()
		s := &cred.Store{Path: filepath.Join(t.TempDir(), "credentials.json")}
		if e != nil {
			if err := s.Save(ctxName, *e); err != nil {
				t.Fatalf("seed store: %v", err)
			}
		}
		return s
	}

	t.Run("no stored entry re-registers", func(t *testing.T) {
		if got := reusableClientID(newStore(t, nil), ctxName, []string{"mcp"}); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("stored client covering scopes is reused", func(t *testing.T) {
		s := newStore(t, &cred.Entry{Type: cred.TypeOAuth, ClientID: "cid-1", Scopes: []string{"mcp", "openid"}})
		if got := reusableClientID(s, ctxName, []string{"mcp"}); got != "cid-1" {
			t.Errorf("got %q, want cid-1", got)
		}
	})
	t.Run("legacy scope-less client is not reused when a scope is needed", func(t *testing.T) {
		s := newStore(t, &cred.Entry{Type: cred.TypeOAuth, ClientID: "cid-legacy"})
		if got := reusableClientID(s, ctxName, []string{"mcp"}); got != "" {
			t.Errorf("got %q, want empty (must re-register)", got)
		}
	})
	t.Run("stored client not covering the needed scope is not reused", func(t *testing.T) {
		s := newStore(t, &cred.Entry{Type: cred.TypeOAuth, ClientID: "cid-2", Scopes: []string{"openid"}})
		if got := reusableClientID(s, ctxName, []string{"mcp"}); got != "" {
			t.Errorf("got %q, want empty (must re-register)", got)
		}
	})
	t.Run("entry without a client_id re-registers", func(t *testing.T) {
		s := newStore(t, &cred.Entry{Type: cred.TypeOAuth, Scopes: []string{"mcp"}})
		if got := reusableClientID(s, ctxName, []string{"mcp"}); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}
