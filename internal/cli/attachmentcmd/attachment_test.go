package attachmentcmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
)

// The behaviour of the built commands is covered end to end in internal/cli;
// these tests live here because Go measures coverage per package. All data is
// fabricated.

const testTeam = "1b2c3d4e-5f6a-4b7c-8d9e-0f1a2b3c4d5e"

func fakeEnv(key string) string {
	if key == cred.EnvAPIKey {
		return "vxk_fabricated"
	}
	return ""
}

// listAttachments returns every attachment in one response and takes no page
// params, so `attachment list` binds no pagination flags and sends only the
// owner.
func TestListIsUnpaginated(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"attachments":[{"id":"att-1"}],"total_count":1}`))
	}))
	defer srv.Close()

	store := &cred.Store{Path: filepath.Join(t.TempDir(), "credentials.json")}
	cmd := newList(func() (*cred.Store, error) { return store, nil }, fakeEnv)
	for _, f := range []string{"limit", "page", "offset", "all"} {
		if cmd.Flags().Lookup(f) != nil {
			t.Errorf("--%s is bound, but the endpoint does not paginate", f)
		}
	}

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--owner-id", "own-1"})
	ctx := clictx.WithRuntime(context.Background(), &config.Runtime{BaseURL: srv.URL, Team: testTeam, Format: "json"})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("list: %v\n%s", err, out.String())
	}
	if want := (url.Values{"owner_type": {"artifact"}, "owner_id": {"own-1"}}); got.Encode() != want.Encode() {
		t.Errorf("query = %v, want only %v", got, want)
	}
}
