package resource

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func summaryStderr(t *testing.T, body string) string {
	t.Helper()
	cmd := &cobra.Command{}
	var errb bytes.Buffer
	cmd.SetErr(&errb)
	cmd.SetOut(&bytes.Buffer{})
	RenderRelationsSummary(cmd, []byte(body))
	return errb.String()
}

func TestRenderRelationsSummaryBoth(t *testing.T) {
	out := summaryStderr(t, `{"related":[{"relation_type":"governed-by","direction":"outgoing","resource_type":"blueprint","title":"Go standards"}],"similar":[{"type":"memory","title":"why pgvector","score":0.82}]}`)
	for _, want := range []string{
		"related (1):", "outgoing governed-by blueprint", "Go standards",
		"similar (1):", `memory "why pgvector" (0.82)`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderRelationsSummaryEmptyAndInvalid(t *testing.T) {
	// No related/similar keys → no output.
	if out := summaryStderr(t, `{"id":"m-1","text":"x"}`); out != "" {
		t.Errorf("expected no summary, got %q", out)
	}
	// Empty arrays → no output.
	if out := summaryStderr(t, `{"related":[],"similar":[]}`); out != "" {
		t.Errorf("expected no summary for empty arrays, got %q", out)
	}
	// Unparseable body → no output, no panic.
	if out := summaryStderr(t, `not json at all`); out != "" {
		t.Errorf("expected no summary for invalid body, got %q", out)
	}
}

func TestRenderRelationsSummaryTruncates(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"related":[`)
	for i := 0; i < 7; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"relation_type":"built-from","direction":"incoming","resource_type":"prompt","title":"p"}`)
	}
	b.WriteString(`]}`)
	out := summaryStderr(t, b.String())
	if !strings.Contains(out, "related (7):") {
		t.Errorf("expected total count 7: %q", out)
	}
	if !strings.Contains(out, "+2 more") {
		t.Errorf("expected truncation tail (+2 more): %q", out)
	}
}
