package authcmd

import (
	"reflect"
	"testing"
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
