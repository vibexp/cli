package cli

import (
	"net/http"
	"net/http/httptest"
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
