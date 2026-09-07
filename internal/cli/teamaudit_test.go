package cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

// Fabricated `GET /{team}/settings/audit` payloads — one fully-populated entry,
// one with every nullable field null (a deleted actor, a deleted source team,
// and a custom_types copy that carries no single resource id).
const (
	auditFullBody = `{"entries":[` +
		`{"id":"e-1","surface":"model_provider","actor_user_id":"u-1","actor_name":"Ada Lovelace",` +
		`"source_team_id":"t-2","source_team_name":"Platform Team","source_resource_id":"r-1",` +
		`"created_resource_id":"r-9","detail":{"source_name":"OpenAI"},"created_at":"2026-08-01T00:00:00Z"},` +
		`{"id":"e-2","surface":"custom_types","actor_user_id":null,"actor_name":null,` +
		`"source_team_id":null,"source_team_name":null,"source_resource_id":null,` +
		`"created_resource_id":null,"detail":{},"created_at":"2026-08-02T00:00:00Z"}` +
		`],"page":1,"per_page":50,"total_count":2,"total_pages":1}`
	auditEmptyBody = `{"entries":[],"page":1,"per_page":50,"total_count":0,"total_pages":1}`
)

// auditServer serves the audit trail for "the-team" and records the last query
// so pagination mapping can be asserted.
func auditServer(t *testing.T, body string, gotQuery *url.Values) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/the-team/settings/audit", func(w http.ResponseWriter, r *http.Request) {
		if gotQuery != nil {
			*gotQuery = r.URL.Query()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	return httptest.NewServer(mux)
}

func TestTeamAuditTSVColumns(t *testing.T) {
	srv := auditServer(t, auditFullBody, nil)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "team", "audit")
	rows := rowsFrom(t, "team audit", out, code, 2)

	// Whole TSV fields, in column order: WHEN, WHO, SURFACE, SOURCE_TEAM,
	// CREATED. Positional rather than slices.Contains, so a column swap fails.
	want := []string{"2026-08-01T00:00:00Z", "Ada Lovelace", "model_provider", "Platform Team", "r-9"}
	if !slices.Equal(rows[0], want) {
		t.Errorf("populated row = %v, want %v", rows[0], want)
	}
	// The all-nulls entry keeps the same column count — a null must not drop a
	// field and shift the row's layout.
	if len(rows[1]) != len(rows[0]) {
		t.Errorf("column count differs between populated (%d) and all-null (%d) rows: %q",
			len(rows[0]), len(rows[1]), out)
	}
}

func TestTeamAuditNullableFieldsRenderEmpty(t *testing.T) {
	srv := auditServer(t, auditFullBody, nil)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "team", "audit")
	row := rowsFrom(t, "team audit", out, code, 2)[1]

	// A deleted actor / deleted source team / custom_types copy renders empty
	// cells — never the literal "null", "<nil>" or "0".
	for i, f := range row {
		if f == "null" || f == "<nil>" || f == "0" {
			t.Errorf("all-null entry field %d = %q, want an empty cell: %v", i, f, row)
		}
	}
	// Only the non-nullable columns carry values.
	if row[0] != "2026-08-02T00:00:00Z" || row[2] != "custom_types" {
		t.Errorf("non-nullable cells wrong: %v", row)
	}
	for _, i := range []int{1, 3, 4} {
		if row[i] != "" {
			t.Errorf("nullable column %d = %q, want empty: %v", i, row[i], row)
		}
	}
}

func TestTeamAuditEmptyLogRendersNoRows(t *testing.T) {
	srv := auditServer(t, auditEmptyBody, nil)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "team", "audit")
	if code != 0 {
		t.Fatalf("empty log exit = %d, out=%q", code, out)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("empty log should render no TSV rows, got %q", out)
	}
}

func TestTeamAuditJSONIsRawBody(t *testing.T) {
	srv := auditServer(t, auditFullBody, nil)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "team", "audit")
	if code != 0 {
		t.Fatalf("exit = %d, out=%q", code, out)
	}
	if out != auditFullBody {
		t.Errorf("json must be the raw response body:\n got %q\nwant %q", out, auditFullBody)
	}
}

func TestTeamAuditPaginationReachesQuery(t *testing.T) {
	var q url.Values
	srv := auditServer(t, auditEmptyBody, &q)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	if _, _, code := runAuth(t, cfg, cs, nil, "", "team", "audit", "--page", "3", "--limit", "7"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if got := q.Get("page"); got != "3" {
		t.Errorf("page = %q, want 3 (query %v)", got, q)
	}
	if got := q.Get("limit"); got != "7" {
		t.Errorf("limit = %q, want 7 (query %v)", got, q)
	}
}

func TestTeamAuditNoTeamIsUsageError(t *testing.T) {
	srv := auditServer(t, auditEmptyBody, nil)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "") // no team anywhere

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "team", "audit")
	if code != exitcode.UsageErr {
		t.Fatalf("no-team exit = %d, want 2", code)
	}
	for _, want := range []string{"--team", "VIBEXP_TEAM", "set-context"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("error should name %q: %q", want, errOut)
		}
	}
}
