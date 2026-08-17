package cli

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// readFieldsServer serves v0.8.0 resource reads carrying the new read-time
// fields: blueprint path/source, and memory related/similar arrays. Fabricated
// data only.
func readFieldsServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Blueprint list — must NOT gain the detail columns.
	mux.HandleFunc("/api/v1/the-team/blueprints", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"blueprints":[{"slug":"imported","title":"Imported BP","type":"cursor","path":".cursor/rules/x.md","updated_at":"2026-07-20T00:00:00Z"}],"page":1,"per_page":50,"total_count":1,"total_pages":1}`))
	})
	// Imported blueprint detail — has path + source provenance.
	mux.HandleFunc("/api/v1/the-team/blueprints/p-1/imported", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"slug":"imported","title":"Imported BP","type":"cursor","path":".cursor/rules/x.md","source":{"repo":"https://github.com/vibexp/vibexp","commit_sha":"6706920221608abcdef","imported_at":"2026-07-20T00:00:00Z"},"updated_at":"2026-07-20T00:00:00Z"}`))
	})
	// Authored blueprint detail — path present, no source.
	mux.HandleFunc("/api/v1/the-team/blueprints/p-1/authored", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"slug":"authored","title":"Authored BP","type":"claude","path":"CLAUDE.md","updated_at":"2026-07-21T00:00:00Z"}`))
	})
	// Memory detail — carries related + similar.
	mux.HandleFunc("/api/v1/the-team/memories/m-1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m-1","project_id":"p-1","status":"active","updated_at":"2026-02-01T00:00:00Z","text":"hi","related":[{"relation_id":"r-1","relation_type":"governed-by","direction":"outgoing","origin":"human","status":"confirmed","resource_type":"blueprint","resource_id":"b-1","title":"Go standards"}],"similar":[{"id":"m-9","type":"memory","title":"why pgvector","score":0.82}]}`))
	})
	return httptest.NewServer(mux)
}

func TestBlueprintDetailColumns(t *testing.T) {
	srv := readFieldsServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Imported blueprint get shows PATH + source provenance.
	out, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "blueprint", "get", "imported")
	if code != 0 {
		t.Fatalf("get imported exit = %d, out=%q", code, out)
	}
	// Piped output is header-less TSV, so assert the column *values* render.
	for _, want := range []string{".cursor/rules/x.md", "https://github.com/vibexp/vibexp", "670692022160"} {
		if !strings.Contains(out, want) {
			t.Errorf("imported detail missing %q: %q", want, out)
		}
	}
	// commit_sha is truncated to 12 chars.
	if strings.Contains(out, "6706920221608abcdef") {
		t.Errorf("commit_sha should be truncated to 12 chars: %q", out)
	}

	// Authored blueprint: PATH present, source columns empty (no panic/error).
	out, _, code = runAuth(t, cfg, cs, nil, "", "--project", "p-1", "blueprint", "get", "authored")
	if code != 0 {
		t.Fatalf("get authored exit = %d, out=%q", code, out)
	}
	if !strings.Contains(out, "CLAUDE.md") {
		t.Errorf("authored detail missing PATH: %q", out)
	}
}

func TestBlueprintListStaysCompact(t *testing.T) {
	srv := readFieldsServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "blueprint", "list")
	if code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	// The list view must NOT render the detail-only columns: its blueprint has a
	// `path` in the JSON, but the compact list columns must not surface it.
	if strings.Contains(out, ".cursor/rules/x.md") {
		t.Errorf("list output must stay compact (path is detail-only): %q", out)
	}
}

func TestMemoryGetShowRelations(t *testing.T) {
	srv := readFieldsServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Without the flag: no summary, stdout carries the resource.
	out, errOut, code := runAuth(t, cfg, cs, nil, "", "memory", "get", "m-1")
	if code != 0 {
		t.Fatalf("get exit = %d", code)
	}
	if strings.Contains(errOut, "related (") {
		t.Errorf("summary must not print without --show-relations: %q", errOut)
	}
	if !strings.Contains(out, "m-1") {
		t.Errorf("stdout should render the resource: %q", out)
	}

	// With the flag: summary goes to stderr, stdout unchanged.
	out, errOut, code = runAuth(t, cfg, cs, nil, "", "memory", "get", "m-1", "--show-relations")
	if code != 0 {
		t.Fatalf("get --show-relations exit = %d", code)
	}
	if !strings.Contains(errOut, "related (1):") || !strings.Contains(errOut, "Go standards") {
		t.Errorf("stderr should carry the related summary: %q", errOut)
	}
	if !strings.Contains(errOut, "similar (1):") || !strings.Contains(errOut, "why pgvector") {
		t.Errorf("stderr should carry the similar summary: %q", errOut)
	}
	if strings.Contains(out, "related (") {
		t.Errorf("summary must go to stderr, not stdout: %q", out)
	}
}

func TestMemoryGetShowRelationsJSONUnchanged(t *testing.T) {
	srv := readFieldsServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// The JSON contract is byte-for-byte identical with and without the flag.
	base, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "memory", "get", "m-1")
	if code != 0 {
		t.Fatalf("json get exit = %d", code)
	}
	withFlag, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "memory", "get", "m-1", "--show-relations")
	if code != 0 {
		t.Fatalf("json get --show-relations exit = %d", code)
	}
	if base != withFlag {
		t.Errorf("--show-relations must not change stdout JSON:\n base=%q\n flag=%q", base, withFlag)
	}
	// And it is the raw body (related/similar pass through).
	if !strings.Contains(base, `"related"`) || !strings.Contains(base, `"similar"`) {
		t.Errorf("json output should carry raw related/similar: %q", base)
	}
}

// staleState is the v0.11.0 ResourceFreshnessState as the platform serialises
// it. It is present ONLY while a resource is stale — absence means fresh, which
// is the branch the renderer has to get right. Fabricated data only.
const staleState = `"freshness":{"status":"stale","since":"2026-08-01T00:00:00Z",` +
	`"matched_rule_ids":["11111111-1111-1111-1111-111111111111","22222222-2222-2222-2222-222222222222"],` +
	`"reason":"rule_run"}`

// freshnessServer serves, for each of the four nouns, a list carrying one stale
// and one fresh object plus a detail read of each.
func freshnessServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	page := `"page":1,"per_page":50,"total_count":2,"total_pages":1`

	write := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}

	memStale := `{"id":"m-stale","project_id":"p-1","status":"active","updated_at":"2026-02-01T00:00:00Z","text":"aging note",` + staleState + `}`
	memFresh := `{"id":"m-fresh","project_id":"p-1","status":"active","updated_at":"2026-02-02T00:00:00Z","text":"current note"}`
	mux.HandleFunc("/api/v1/the-team/memories", func(w http.ResponseWriter, _ *http.Request) {
		write(w, `{"memories":[`+memStale+`,`+memFresh+`],`+page+`}`)
	})
	mux.HandleFunc("/api/v1/the-team/memories/m-stale", func(w http.ResponseWriter, _ *http.Request) { write(w, memStale) })
	mux.HandleFunc("/api/v1/the-team/memories/m-fresh", func(w http.ResponseWriter, _ *http.Request) { write(w, memFresh) })

	promptStale := `{"slug":"p-stale","name":"Aging prompt","status":"published","updated_at":"2026-02-01T00:00:00Z",` + staleState + `}`
	promptFresh := `{"slug":"p-fresh","name":"Current prompt","status":"published","updated_at":"2026-02-02T00:00:00Z"}`
	mux.HandleFunc("/api/v1/the-team/prompts", func(w http.ResponseWriter, _ *http.Request) {
		write(w, `{"prompts":[`+promptStale+`,`+promptFresh+`],`+page+`}`)
	})
	mux.HandleFunc("/api/v1/the-team/prompts/p-stale", func(w http.ResponseWriter, _ *http.Request) { write(w, promptStale) })
	mux.HandleFunc("/api/v1/the-team/prompts/p-fresh", func(w http.ResponseWriter, _ *http.Request) { write(w, promptFresh) })

	bpStale := `{"slug":"b-stale","title":"Aging BP","type":"claude","path":"CLAUDE.md","updated_at":"2026-02-01T00:00:00Z",` + staleState + `}`
	bpFresh := `{"slug":"b-fresh","title":"Current BP","type":"claude","path":"AGENTS.md","updated_at":"2026-02-02T00:00:00Z"}`
	mux.HandleFunc("/api/v1/the-team/blueprints", func(w http.ResponseWriter, _ *http.Request) {
		write(w, `{"blueprints":[`+bpStale+`,`+bpFresh+`],`+page+`}`)
	})
	mux.HandleFunc("/api/v1/the-team/blueprints/p-1/b-stale", func(w http.ResponseWriter, _ *http.Request) { write(w, bpStale) })
	mux.HandleFunc("/api/v1/the-team/blueprints/p-1/b-fresh", func(w http.ResponseWriter, _ *http.Request) { write(w, bpFresh) })

	artStale := `{"slug":"a-stale","title":"Aging report","project_id":"p-1","updated_at":"2026-02-01T00:00:00Z",` + staleState + `}`
	artFresh := `{"slug":"a-fresh","title":"Current report","project_id":"p-1","updated_at":"2026-02-02T00:00:00Z"}`
	mux.HandleFunc("/api/v1/the-team/artifacts", func(w http.ResponseWriter, _ *http.Request) {
		write(w, `{"artifacts":[`+artStale+`,`+artFresh+`],`+page+`}`)
	})
	mux.HandleFunc("/api/v1/the-team/artifacts/p-1/a-stale", func(w http.ResponseWriter, _ *http.Request) { write(w, artStale) })
	mux.HandleFunc("/api/v1/the-team/artifacts/p-1/a-fresh", func(w http.ResponseWriter, _ *http.Request) { write(w, artFresh) })

	return httptest.NewServer(mux)
}

// tsvRows splits header-less piped output into rows of fields.
func tsvRows(out string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
	}
	return rows
}

// TestFreshnessListColumn asserts every noun's list carries the STALE flag on a
// stale row, leaves it empty on a fresh one, and keeps a stable column count
// across the two — the flag must never shift a row's field layout.
func TestFreshnessListColumn(t *testing.T) {
	srv := freshnessServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	for _, noun := range []string{"memory", "prompt", "blueprint", "artifact"} {
		t.Run(noun, func(t *testing.T) {
			out, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", noun, "list")
			if code != 0 {
				t.Fatalf("list exit = %d, out=%q", code, out)
			}
			rows := tsvRows(out)
			if len(rows) != 2 {
				t.Fatalf("got %d rows, want 2 (one stale, one fresh): %q", len(rows), out)
			}
			if len(rows[0]) != len(rows[1]) {
				t.Errorf("column count differs between stale (%d) and fresh (%d) rows: %q",
					len(rows[0]), len(rows[1]), out)
			}
			staleIdx := -1
			for i, f := range rows[0] {
				if f == "stale" {
					staleIdx = i
				}
			}
			if staleIdx < 0 {
				t.Fatalf("stale row has no %q cell: %v", "stale", rows[0])
			}
			// Absent freshness renders as an empty cell, never "null"/"<nil>".
			if got := rows[1][staleIdx]; got != "" {
				t.Errorf("fresh row STALE cell = %q, want empty", got)
			}
		})
	}
}

// TestFreshnessDetailColumns asserts the full stale state reaches detail output
// and that a fresh resource leaves every one of those cells empty — including
// STALE_RULES, which a bare `| length` would render as a misleading "0".
func TestFreshnessDetailColumns(t *testing.T) {
	srv := freshnessServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	cases := []struct{ noun, stale, fresh string }{
		{"memory", "m-stale", "m-fresh"},
		{"prompt", "p-stale", "p-fresh"},
		{"blueprint", "b-stale", "b-fresh"},
		{"artifact", "a-stale", "a-fresh"},
	}
	for _, c := range cases {
		t.Run(c.noun, func(t *testing.T) {
			out, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", c.noun, "get", c.stale)
			if code != 0 {
				t.Fatalf("get stale exit = %d, out=%q", code, out)
			}
			// Match whole TSV fields, not substrings: the fixture slugs contain
			// "stale" and every timestamp contains "2", so strings.Contains
			// would pass on output carrying no freshness at all.
			staleRows := tsvRows(out)
			if len(staleRows) != 1 {
				t.Fatalf("got %d rows, want 1: %q", len(staleRows), out)
			}
			for _, want := range []string{"stale", "2026-08-01T00:00:00Z", "rule_run", "2"} {
				if !slices.Contains(staleRows[0], want) {
					t.Errorf("stale detail has no %q cell: %v", want, staleRows[0])
				}
			}

			out, _, code = runAuth(t, cfg, cs, nil, "", "--project", "p-1", c.noun, "get", c.fresh)
			if code != 0 {
				t.Fatalf("get fresh exit = %d, out=%q", code, out)
			}
			rows := tsvRows(out)
			if len(rows) != 1 {
				t.Fatalf("got %d rows, want 1: %q", len(rows), out)
			}
			for i, f := range rows[0] {
				// "0" would be the bare-`length` bug: it reads as "evaluated,
				// no rules matched" rather than "not stale".
				if f == "0" || f == "null" || f == "<nil>" {
					t.Errorf("fresh detail field %d = %q, want an empty cell: %v", i, f, rows[0])
				}
			}
			if strings.Contains(out, "stale") || strings.Contains(out, "rule_run") {
				t.Errorf("fresh detail leaked stale state: %q", out)
			}
		})
	}
}

// TestFreshnessJSONIsRawBody guards the output contract: JSON is the API
// response body, so the curated columns must add no mapping layer.
func TestFreshnessJSONIsRawBody(t *testing.T) {
	srv := freshnessServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "memory", "get", "m-stale")
	if code != 0 {
		t.Fatalf("json get exit = %d", code)
	}
	want := `{"id":"m-stale","project_id":"p-1","status":"active","updated_at":"2026-02-01T00:00:00Z","text":"aging note",` + staleState + `}`
	if strings.TrimSpace(out) != want {
		t.Errorf("json output is not the raw body:\n got=%q\nwant=%q", strings.TrimSpace(out), want)
	}
}
