package resource

import (
	"encoding/json"
	"testing"

	"github.com/itchyny/gojq"

	"github.com/vibexp/cli/internal/output"
)

// runPath evaluates a column's gojq path against a JSON document the way the
// renderer does, so these tests exercise the real expressions rather than a
// paraphrase of them.
func runPath(t *testing.T, path, doc string) any {
	t.Helper()
	q, err := gojq.Parse(path)
	if err != nil {
		t.Fatalf("parse %q: %v", path, err)
	}
	var v any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("unmarshal %q: %v", doc, err)
	}
	got, ok := q.Run(v).Next()
	if !ok {
		return nil
	}
	if err, isErr := got.(error); isErr {
		t.Fatalf("run %q: %v", path, err)
	}
	return got
}

func TestFreshnessColumnHeaders(t *testing.T) {
	if got := FreshnessColumn().Header; got != "STALE" {
		t.Errorf("list column header = %q, want STALE", got)
	}
	var headers []string
	for _, c := range FreshnessDetailColumns() {
		headers = append(headers, c.Header)
	}
	want := []string{"STALE", "STALE_SINCE", "STALE_REASON", "STALE_RULES"}
	if len(headers) != len(want) {
		t.Fatalf("detail headers = %v, want %v", headers, want)
	}
	for i := range want {
		if headers[i] != want[i] {
			t.Errorf("detail header %d = %q, want %q", i, headers[i], want[i])
		}
	}
}

// TestFreshnessDetailPaths pins the behaviour the whole feature hinges on: a
// resource carries freshness only while it is stale, so every path must yield
// nothing on a fresh resource — in particular STALE_RULES must not yield 0,
// which would read as "evaluated, no rules matched" rather than "not stale".
func TestFreshnessDetailPaths(t *testing.T) {
	const stale = `{"freshness":{"status":"stale","since":"2026-08-01T00:00:00Z",` +
		`"matched_rule_ids":["a","b","c"],"reason":"rule_run"}}`
	const fresh = `{"id":"m-1"}`

	byHeader := map[string]output.Column{}
	for _, c := range FreshnessDetailColumns() {
		byHeader[c.Header] = c
	}

	staleWant := map[string]any{
		"STALE":        "stale",
		"STALE_SINCE":  "2026-08-01T00:00:00Z",
		"STALE_REASON": "rule_run",
		"STALE_RULES":  "3",
	}
	for header, want := range staleWant {
		if got := runPath(t, byHeader[header].Path, stale); got != want {
			t.Errorf("%s on a stale resource = %#v, want %#v", header, got, want)
		}
	}
	for header := range staleWant {
		got := runPath(t, byHeader[header].Path, fresh)
		// nil is what the renderer stringifies to an empty cell; "" is the
		// guard's own else-branch. Anything else (notably float64(0)) is a bug.
		if got != nil && got != "" {
			t.Errorf("%s on a fresh resource = %#v, want nil or empty", header, got)
		}
	}
}

// TestWithFreshnessDetailSplicesAndDoesNotAlias checks the ordering contract
// and that the helper cannot corrupt a caller's slice — the noun packages pass
// literals that must not gain the freshness columns by aliasing.
func TestWithFreshnessDetailSplicesAndDoesNotAlias(t *testing.T) {
	head := []output.Column{{Header: "SLUG", Path: ".slug"}}
	tail := []output.Column{{Header: "UPDATED", Path: ".updated_at"}}

	got := WithFreshnessDetail(head, tail)
	want := []string{"SLUG", "STALE", "STALE_SINCE", "STALE_REASON", "STALE_RULES", "UPDATED"}
	if len(got) != len(want) {
		t.Fatalf("got %d columns, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Header != want[i] {
			t.Errorf("column %d = %q, want %q", i, got[i].Header, want[i])
		}
	}

	if len(head) != 1 || len(tail) != 1 {
		t.Errorf("inputs were mutated: head=%v tail=%v", head, tail)
	}
	// Two calls must not share backing state, or one noun editing its columns
	// would silently edit another's.
	got[1].Header = "MUTATED"
	if FreshnessDetailColumns()[0].Header != "STALE" {
		t.Error("FreshnessDetailColumns returns shared state; a caller mutated the source")
	}
	if second := WithFreshnessDetail(head, tail); second[1].Header != "STALE" {
		t.Errorf("a second splice saw the first's mutation: %q", second[1].Header)
	}
}
