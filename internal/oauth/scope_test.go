package oauth

import (
	"reflect"
	"testing"
)

func TestNegotiateScopes(t *testing.T) {
	preferred := []string{"mcp"}

	tests := []struct {
		name      string
		preferred []string
		supported []string
		override  []string
		want      []string
	}{
		{
			name:      "mcp advertised is requested",
			preferred: preferred,
			supported: []string{"mcp", "openid"},
			want:      []string{"mcp"},
		},
		{
			name:      "mcp absent is dropped",
			preferred: preferred,
			supported: []string{"openid", "profile"},
			want:      nil,
		},
		{
			name:      "empty supported omits scope",
			preferred: preferred,
			supported: nil,
			want:      nil,
		},
		{
			name:      "override wins verbatim over negotiation",
			preferred: preferred,
			supported: []string{"mcp"},
			override:  []string{"api", "openid"},
			want:      []string{"api", "openid"},
		},
		{
			name:      "override wins even when unadvertised",
			preferred: preferred,
			supported: []string{"openid"},
			override:  []string{"custom-scope"},
			want:      []string{"custom-scope"},
		},
		{
			name:      "override is trimmed and de-duplicated",
			preferred: preferred,
			supported: nil,
			override:  []string{" api ", "api", "", "openid"},
			want:      []string{"api", "openid"},
		},
		{
			name:      "intersection preserves preferred order",
			preferred: []string{"mcp", "openid"},
			supported: []string{"openid", "mcp", "profile"},
			want:      []string{"mcp", "openid"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NegotiateScopes(tc.preferred, tc.supported, tc.override)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("NegotiateScopes(%v, %v, %v) = %v, want %v",
					tc.preferred, tc.supported, tc.override, got, tc.want)
			}
		})
	}
}
