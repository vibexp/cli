package blueprintcmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

// TestLabelWrite drives --label through this package's create/update (the
// helper itself is covered in internal/cli/resource). All data is fabricated.
func TestLabelWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	resolve := func() (*cred.Store, error) { return &cred.Store{Path: path}, nil }
	tests := []struct {
		name    string
		update  bool
		args    []string
		want    string // labels sent, "" = key absent
		methods string
		code    int
	}{
		{"create", false, []string{"bp-1", "--title", "T", "--body-file", "-", "--label", "a", "--label", "b"}, `["a","b"]`, "POST", 0},
		{"create too long", false, []string{"bp-1", "--title", "T", "--body-file", "-", "--label", strings.Repeat("x", 51)}, "", "", exitcode.UsageErr},
		{"update sets", true, []string{"bp-1", "--label", "c"}, `["c"]`, "PUT", 0},
		{"update clears", true, []string{"bp-1", "--label", ""}, `[]`, "PUT", 0},
		{"update without --label keeps labels", true, []string{"bp-1", "--status", "active"}, "", "PUT", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			var methods []string
			srv := metaServer(t, &body, &methods)
			defer srv.Close()
			cmd := newCreate(resolve, fakeEnv)
			if tc.update {
				cmd = newUpdate(resolve, fakeEnv)
			}
			if code := runMeta(t, srv, cmd, "content", tc.args...); code != tc.code {
				t.Fatalf("exit = %d, want %d", code, tc.code)
			}
			if got := strings.Join(methods, " "); got != tc.methods {
				t.Errorf("requests = %q, want %q", got, tc.methods)
			}
			got, ok := body["labels"]
			if tc.want == "" {
				if ok {
					t.Errorf("labels = %v, want the key absent", got)
				}
				return
			}
			if b, _ := json.Marshal(got); string(b) != tc.want {
				t.Errorf("labels = %s, want %s", b, tc.want)
			}
		})
	}
}
