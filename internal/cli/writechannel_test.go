package cli

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// writeChannelCases pairs each noun with an update invocation
// and the full detail header row it must render: every existing column in its
// existing position, then WRITTEN_VIA and ASSISTANT. Adding a noun is a row.
var writeChannelCases = []struct {
	noun    string
	update  []string
	headers []string
}{
	{
		"memory", []string{"m-1", "--status", "active"},
		[]string{"ID", "PROJECT", "STATUS", "STALE", "STALE_SINCE", "STALE_REASON", "STALE_RULES",
			"UPDATED", "TEXT", "TITLE", "WRITTEN_VIA", "ASSISTANT"},
	},
	{
		"prompt", []string{"p-1", "--description", "d"},
		[]string{"SLUG", "NAME", "STATUS", "STALE", "STALE_SINCE", "STALE_REASON", "STALE_RULES",
			"UPDATED", "LOCKED", "LOCKED_BY", "LOCKED_AT", "LOCK_COMMENT", "WRITTEN_VIA", "ASSISTANT"},
	},
	{
		"blueprint", []string{"b-1", "--description", "d"},
		[]string{"SLUG", "TITLE", "TYPE", "PATH", "SOURCE_REPO", "SOURCE_COMMIT",
			"STALE", "STALE_SINCE", "STALE_REASON", "STALE_RULES",
			"UPDATED", "LOCKED", "LOCKED_BY", "LOCKED_AT", "LOCK_COMMENT", "WRITTEN_VIA", "ASSISTANT"},
	},
	{
		"artifact", []string{"a-1", "--description", "d"},
		[]string{"SLUG", "TITLE", "PROJECT", "STALE", "STALE_SINCE", "STALE_REASON", "STALE_RULES",
			"UPDATED", "LOCKED", "LOCKED_BY", "LOCKED_AT", "LOCK_COMMENT", "WRITTEN_VIA", "ASSISTANT"},
	},
}

// writeChannelServer answers every method on any team-scoped path with body,
// so get, list and update all read the same fabricated object back.
func writeChannelServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasPrefix(r.URL.Path, "/api/v1/"+testTeamID+"/") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

// writeChannelBody is one object carrying every noun's identifying fields plus
// the given write-channel fragment (empty = the fields are absent).
func writeChannelBody(fragment string) string {
	b := `{"id":"m-1","slug":"x-1","name":"N","title":"T","type":"claude","project_id":"` + testProjectID +
		`","status":"active","text":"hi","updated_at":"2026-02-01T00:00:00Z"`
	if fragment != "" {
		b += "," + fragment
	}
	return b + "}"
}

// headerRow returns the first line of table output split into header names.
func headerRow(out string) []string {
	first, _, _ := strings.Cut(out, "\n")
	return strings.Fields(first)
}

// TestWriteChannelDetailHeaders pins the full header row of get and of the
// update confirmation per noun: the write channel comes last, and no existing
// column moves.
func TestWriteChannelDetailHeaders(t *testing.T) {
	srv := writeChannelServer(t, writeChannelBody(`"last_write_source":"mcp","last_write_assistant_name":"claude-code"`))
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	for _, c := range writeChannelCases {
		t.Run(c.noun, func(t *testing.T) {
			for _, args := range [][]string{{"get", "x-1"}, append([]string{"update"}, c.update...)} {
				full := append([]string{"--project", testProjectID, "--format", "table", c.noun}, args...)
				out, errOut, code := runAuth(t, cfg, cs, nil, "", full...)
				if code != 0 {
					t.Fatalf("%s %s exit = %d, stderr=%q", c.noun, args[0], code, errOut)
				}
				if got := headerRow(out); !slices.Equal(got, c.headers) {
					t.Errorf("%s %s headers =\n %v\nwant\n %v", c.noun, args[0], got, c.headers)
				}
			}
		})
	}
}

// TestWriteChannelDetailValues asserts the server's values reach the last two
// cells verbatim, and that a null or absent field renders an empty cell —
// never "null", and never a guessed channel.
func TestWriteChannelDetailValues(t *testing.T) {
	for _, tc := range []struct {
		name, fragment, via, assistant string
	}{
		{"mcp with assistant", `"last_write_source":"mcp","last_write_assistant_name":"Claude Code"`, "mcp", "Claude Code"},
		{"cli without assistant", `"last_write_source":"cli","last_write_assistant_name":null`, "cli", ""},
		{"not recorded", `"last_write_source":null,"last_write_assistant_name":null`, "", ""},
		{"pre-v0.16.0 server", ``, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := writeChannelServer(t, writeChannelBody(tc.fragment))
			defer srv.Close()
			cfg, cs := apiFixture(t, srv.URL, testTeamID)
			for _, c := range writeChannelCases {
				out, _, code := runAuth(t, cfg, cs, nil, "", "--project", testProjectID, c.noun, "get", "x-1")
				row := rowsFrom(t, c.noun+" get", out, code, 1)[0]
				if len(row) != len(c.headers) {
					t.Fatalf("%s row has %d cells, want %d: %q", c.noun, len(row), len(c.headers), row)
				}
				if got := row[len(row)-2:]; got[0] != tc.via || got[1] != tc.assistant {
					t.Errorf("%s WRITTEN_VIA, ASSISTANT = %q, want [%q %q]", c.noun, got, tc.via, tc.assistant)
				}
			}
		})
	}
}

// TestWriteChannelListUnchanged guards the list views: the write channel is
// detail-only, so a list row never carries it.
func TestWriteChannelListUnchanged(t *testing.T) {
	item := writeChannelBody(`"last_write_source":"mcp","last_write_assistant_name":"claude-code"`)
	page := `"page":1,"per_page":50,"total_count":1,"total_pages":1`
	srv := writeChannelServer(t, `{"memories":[`+item+`],"prompts":[`+item+`],"blueprints":[`+item+`],"artifacts":[`+item+`],`+page+`}`)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	for _, c := range writeChannelCases {
		out, _, code := runAuth(t, cfg, cs, nil, "", "--project", testProjectID, "--format", "table", c.noun, "list")
		if code != 0 {
			t.Fatalf("%s list exit = %d", c.noun, code)
		}
		// A header-only output would make the leak check below vacuous.
		if lines := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1; lines != 2 {
			t.Fatalf("%s list rendered %d lines, want a header and one row: %q", c.noun, lines, out)
		}
		if h := headerRow(out); slices.Contains(h, "WRITTEN_VIA") || slices.Contains(h, "ASSISTANT") {
			t.Errorf("%s list headers carry the write channel: %v", c.noun, h)
		}
		if strings.Contains(out, "claude-code") || strings.Contains(out, "mcp") {
			t.Errorf("%s list leaked the write channel: %q", c.noun, out)
		}
	}
}
