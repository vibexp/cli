package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

const fixture = `{"items":[{"id":"a1","name":"Alpha","active":true},{"id":"b2","name":"Beta","active":false}]}`

func listSpec() *TableSpec {
	return &TableSpec{
		Rows: ".items[]",
		Columns: []Column{
			{Header: "ID", Path: ".id"},
			{Header: "NAME", Path: ".name"},
			{Header: "ACTIVE", Path: ".active"},
		},
	}
}

func render(t *testing.T, raw string, spec *TableSpec, opts Options) string {
	t.Helper()
	var b bytes.Buffer
	if err := Render(&b, []byte(raw), spec, opts); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return b.String()
}

func TestRenderTableNoColor(t *testing.T) {
	out := render(t, fixture, listSpec(), Options{Format: FormatTable, IsTTY: true, Color: false})
	for _, want := range []string{"ID", "NAME", "ACTIVE", "a1", "Alpha", "true", "b2", "Beta", "false"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("table without color must have no ANSI:\n%q", out)
	}
	// Header precedes data.
	if strings.Index(out, "ID") > strings.Index(out, "a1") {
		t.Error("header should precede data rows")
	}
}

func TestRenderTableColorHeaders(t *testing.T) {
	out := render(t, fixture, listSpec(), Options{Format: FormatTable, IsTTY: true, Color: true})
	if !strings.Contains(out, ansiBold) {
		t.Errorf("colored table should bold headers, got:\n%q", out)
	}
}

func TestRenderTSVWhenPiped(t *testing.T) {
	// Auto format + not a TTY → TSV, no header, tab-separated, no color.
	out := render(t, fixture, listSpec(), Options{Format: FormatAuto, IsTTY: false})
	if strings.Contains(out, "\x1b") {
		t.Errorf("TSV must have no ANSI: %q", out)
	}
	if strings.Contains(out, "ID") {
		t.Errorf("TSV must omit headers: %q", out)
	}
	if out != "a1\tAlpha\ttrue\nb2\tBeta\tfalse\n" {
		t.Errorf("TSV mismatch:\n%q", out)
	}
}

func TestRenderJSONByteIdentical(t *testing.T) {
	out := render(t, fixture, nil, Options{Format: FormatJSON})
	if out != fixture {
		t.Errorf("JSON must be byte-identical to the response body:\n got %q\nwant %q", out, fixture)
	}
}

func TestRenderYAML(t *testing.T) {
	out := render(t, fixture, nil, Options{Format: FormatYAML})
	if !strings.Contains(out, "items:") || !strings.Contains(out, "id: a1") || !strings.Contains(out, "active: true") {
		t.Errorf("YAML conversion wrong:\n%s", out)
	}
}

func TestRenderJQFiltersList(t *testing.T) {
	out := render(t, fixture, listSpec(), Options{Format: FormatTable, JQ: ".items[].id"})
	// --jq forces JSON even with a table format; results are newline-delimited.
	if out != "\"a1\"\n\"b2\"\n" {
		t.Errorf("jq output mismatch:\n%q", out)
	}
}

func TestRenderJQObjectExpression(t *testing.T) {
	out := render(t, fixture, nil, Options{JQ: ".items | length"})
	if strings.TrimSpace(out) != "2" {
		t.Errorf("jq length = %q, want 2", out)
	}
}

func TestRenderJQYAML(t *testing.T) {
	out := render(t, fixture, nil, Options{Format: FormatYAML, JQ: ".items[0]"})
	if !strings.Contains(out, "id: a1") {
		t.Errorf("jq+yaml wrong:\n%s", out)
	}
}

func TestRenderJQInvalidExpressionExit2(t *testing.T) {
	var b bytes.Buffer
	err := Render(&b, []byte(fixture), nil, Options{JQ: ".items[("})
	if got := exitcode.FromError(err); got != exitcode.UsageErr {
		t.Errorf("invalid jq exit = %d, want 2", got)
	}
}

func TestByteStableAcrossRuns(t *testing.T) {
	opts := Options{Format: FormatTable, IsTTY: true, Color: true}
	a := render(t, fixture, listSpec(), opts)
	b := render(t, fixture, listSpec(), opts)
	if a != b {
		t.Error("table render not byte-stable across runs")
	}
}

func TestParseFormatInvalidIsUsage(t *testing.T) {
	_, err := ParseFormat("xml")
	if got := exitcode.FromError(err); got != exitcode.UsageErr {
		t.Errorf("invalid format exit = %d, want 2", got)
	}
	for _, ok := range []string{"", "json", "yaml", "table", "text"} {
		if _, err := ParseFormat(ok); err != nil {
			t.Errorf("ParseFormat(%q) unexpected error: %v", ok, err)
		}
	}
}

func TestColorEnabled(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	if ColorEnabled(true, env(map[string]string{"NO_COLOR": "1"})) {
		t.Error("NO_COLOR must disable color")
	}
	if ColorEnabled(true, env(map[string]string{"TERM": "dumb"})) {
		t.Error("TERM=dumb must disable color")
	}
	if !ColorEnabled(true, env(nil)) {
		t.Error("TTY with clean env should enable color")
	}
	if ColorEnabled(false, env(nil)) {
		t.Error("non-TTY must never color")
	}
}
