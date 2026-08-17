package promptcmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/output"
)

// listPrompts nests its array under `data`, unlike every other list endpoint.
// Fabricated, per repo convention — never captured from a deployment.
const nestedEnvelope = `{"data":{"page":1,"per_page":20,"total_count":2,"total_pages":1,"prompts":[
	{"slug":"alpha","name":"Alpha","status":"published","freshness":"active","updated_at":"2026-01-02T03:04:05Z"},
	{"slug":"beta","name":"Beta","status":"draft","freshness":"stale","updated_at":"2026-01-03T03:04:05Z"}]},
	"message":"Prompts retrieved successfully","status":"success"}`

const nestedEnvelopeEmpty = `{"data":{"page":1,"per_page":20,"total_count":0,"total_pages":0,"prompts":[]},
	"message":"Prompts retrieved successfully","status":"success"}`

func renderList(t *testing.T, raw string) string {
	t.Helper()
	var b bytes.Buffer
	spec := listSpec
	if err := output.Render(&b, []byte(raw), &spec, output.Options{Format: output.FormatAuto}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return b.String()
}

// The `data` envelope must yield one row per prompt. Before #72 the row spec
// fell through to `.data[]?`, which iterates the object's values and emitted a
// blank row per envelope field instead.
func TestListSpecRendersNestedEnvelope(t *testing.T) {
	out := renderList(t, nestedEnvelope)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 rows, got %d:\n%q", len(lines), out)
	}
	for _, want := range []string{"alpha", "Alpha", "published", "beta", "Beta", "draft"} {
		if !strings.Contains(out, want) {
			t.Errorf("row output missing %q:\n%s", want, out)
		}
	}
	// Envelope metadata must never leak into the rows.
	for _, unwanted := range []string{"per_page", "total_count", "Prompts retrieved successfully"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("envelope field %q leaked into rows:\n%s", unwanted, out)
		}
	}
}

// An empty list writes nothing to stdout — scripts count rows, so phantom
// blank rows are a contract break.
func TestListSpecEmptyListWritesNothing(t *testing.T) {
	if out := renderList(t, nestedEnvelopeEmpty); out != "" {
		t.Errorf("empty list must write nothing, got %q", out)
	}
}
