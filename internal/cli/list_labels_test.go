package cli

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

// TestListLabelsFilter covers the v0.13.0 labels filter on all four curated
// list commands: repeated --labels are sent as one comma-joined labels= param
// (the platform ORs them), and an unfiltered list sends no labels param.
func TestListLabelsFilter(t *testing.T) {
	nouns := map[string]func(t *testing.T) (serverURL string, query func() url.Values, done func()){
		"memory": func(t *testing.T) (string, func() url.Values, func()) {
			var cap memoryCapture
			srv := memoryServer(t, &cap)
			return srv.URL, func() url.Values { return cap.listQuery }, srv.Close
		},
		"artifact": func(t *testing.T) (string, func() url.Values, func()) {
			var cap artifactCapture
			srv := artifactServer(t, &cap)
			return srv.URL, func() url.Values { return cap.listQuery }, srv.Close
		},
		"blueprint": func(t *testing.T) (string, func() url.Values, func()) {
			var cap blueprintCapture
			srv := blueprintServer(t, &cap)
			return srv.URL, func() url.Values { return cap.listQuery }, srv.Close
		},
		"prompt": func(t *testing.T) (string, func() url.Values, func()) {
			var cap promptCapture
			srv := promptServer(t, &cap)
			return srv.URL, func() url.Values { return cap.listQuery }, srv.Close
		},
	}
	for noun, start := range nouns {
		t.Run(noun, func(t *testing.T) {
			for _, tc := range []struct {
				args []string
				want string // "" = param absent
			}{
				{[]string{"--labels", "a", "--labels", "b"}, "a,b"},
				{nil, ""},
			} {
				base, query, done := start(t)
				cfg, cs := apiFixture(t, base, "the-team")
				args := append([]string{"--format", "json", "--project", "p-1", noun, "list"}, tc.args...)
				if _, _, code := runAuth(t, cfg, cs, nil, "", args...); code != 0 {
					done()
					t.Fatalf("%v: exit = %d", tc.args, code)
				}
				q := query()
				done()
				got, ok := q["labels"]
				switch {
				case tc.want == "" && ok:
					t.Errorf("labels param present on an unfiltered list: %v", got)
				case tc.want != "" && q.Get("labels") != tc.want:
					t.Errorf("labels param = %q, want %q", q.Get("labels"), tc.want)
				}
			}
		})
	}
}

// TestLabelLimitsAreUsageErrors pins the --label limits through the root
// command: a label over 50 characters, or an 11th label, is a flag error →
// exit 2, and no request is sent.
func TestLabelLimitsAreUsageErrors(t *testing.T) {
	var many []string
	for i := 0; i <= 10; i++ {
		many = append(many, "--label", fmt.Sprintf("l%d", i))
	}
	for name, labelArgs := range map[string][]string{
		"too long": {"--label", strings.Repeat("x", 51)},
		"too many": many,
	} {
		t.Run(name, func(t *testing.T) {
			var cap memoryCapture
			srv := memoryServer(t, &cap)
			defer srv.Close()
			cfg, cs := apiFixture(t, srv.URL, "the-team")
			args := append([]string{"--project", "p-1", "memory", "create", "--body-file", "-"}, labelArgs...)
			if _, _, code := runAuth(t, cfg, cs, nil, "content", args...); code != exitcode.UsageErr {
				t.Fatalf("exit = %d, want %d", code, exitcode.UsageErr)
			}
			if cap.createBody != nil {
				t.Errorf("a request was sent: %v", cap.createBody)
			}
		})
	}
}
