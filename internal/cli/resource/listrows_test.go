package resource_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/output"
)

// Every noun that has a list view, with the array key its endpoint uses.
// Fabricated throughout, per repo convention.
var nouns = []struct {
	name string
	keys []string
}{
	{"memory", []string{"memories"}},
	{"blueprint", []string{"blueprints"}},
	{"artifact", []string{"artifacts"}},
	{"prompt", []string{"prompts"}},
	{"feed", []string{"feeds"}},
	{"feed items", []string{"items", "feed_items"}},
	{"feed replies", []string{"replies"}},
	{"project", []string{"projects"}},
	{"team", []string{"teams"}},
	{"relation", []string{"relations"}},
	{"attachment", []string{"attachments"}},
	{"search", []string{"results"}},
}

func spec(keys []string) *output.TableSpec {
	return &output.TableSpec{
		Rows: resource.ListRows(keys...),
		Columns: []output.Column{
			{Header: "ID", Path: ".id"},
			{Header: "NAME", Path: ".name"},
		},
	}
}

func render(t *testing.T, raw string, keys []string) string {
	t.Helper()
	var b bytes.Buffer
	if err := output.Render(&b, []byte(raw), spec(keys), output.Options{Format: output.FormatAuto}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return b.String()
}

const rows = `[{"id":"a1","name":"Alpha"},{"id":"b2","name":"Beta"}]`

// topLevel is the shape most endpoints return; nested is the envelope
// listPrompts returns. Both must yield one row per item.
func topLevel(key, arr string) string {
	return fmt.Sprintf(`{"%s":%s,"page":1,"per_page":20,"total_count":2,"total_pages":1}`, key, arr)
}

func nested(key, arr string) string {
	return fmt.Sprintf(`{"data":{"%s":%s,"page":1,"per_page":20,"total_count":2,"total_pages":1},`+
		`"message":"retrieved","status":"success"}`, key, arr)
}

func TestListRowsRendersBothEnvelopeShapes(t *testing.T) {
	for _, n := range nouns {
		for _, env := range []struct {
			shape string
			build func(string, string) string
		}{{"top-level", topLevel}, {"nested", nested}} {
			t.Run(n.name+"/"+env.shape, func(t *testing.T) {
				out := render(t, env.build(n.keys[0], rows), n.keys)

				lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
				if len(lines) != 2 {
					t.Fatalf("want 2 rows, got %d:\n%q", len(lines), out)
				}
				for _, want := range []string{"a1", "Alpha", "b2", "Beta"} {
					if !strings.Contains(out, want) {
						t.Errorf("missing %q:\n%s", want, out)
					}
				}
				// The regression behind #72: envelope fields must never become rows.
				for _, leak := range []string{"per_page", "total_count", "retrieved", "success"} {
					if strings.Contains(out, leak) {
						t.Errorf("envelope field %q leaked into rows:\n%s", leak, out)
					}
				}
			})
		}
	}
}

// An empty list writes nothing. `//` falls through when its left side yields no
// values, so an empty array is exactly the case that used to reach a bare
// `.data[]?` and emit one blank row per envelope field.
func TestListRowsEmptyListWritesNothing(t *testing.T) {
	for _, n := range nouns {
		for _, env := range []struct {
			shape string
			build func(string, string) string
		}{{"top-level", topLevel}, {"nested", nested}} {
			t.Run(n.name+"/"+env.shape, func(t *testing.T) {
				if out := render(t, env.build(n.keys[0], "[]"), n.keys); out != "" {
					t.Errorf("empty list must write nothing, got %q", out)
				}
			})
		}
	}
}

// An envelope we do not recognise renders zero rows rather than garbage — the
// property that keeping every alternative key-indexed buys us.
func TestListRowsUnknownShapeRendersNothing(t *testing.T) {
	unknown := []string{
		`{"unexpected":[{"id":"a1","name":"Alpha"}],"page":1,"total_count":1}`,
		`{"data":{"unexpected":[{"id":"a1","name":"Alpha"}],"page":1},"status":"success"}`,
		`{"page":1,"per_page":20,"total_count":0}`,
		`{"data":{"page":1,"per_page":20,"total_count":0},"message":"none","status":"success"}`,
	}
	for _, n := range nouns {
		for i, raw := range unknown {
			t.Run(fmt.Sprintf("%s/%d", n.name, i), func(t *testing.T) {
				if out := render(t, raw, n.keys); out != "" {
					t.Errorf("unknown shape must render nothing, got %q", out)
				}
			})
		}
	}
}

// Extra keys keep the precedence they are given, so a noun that spells its
// array more than one way still prefers its primary key.
func TestListRowsKeyPrecedenceAndAliases(t *testing.T) {
	got := resource.ListRows("items", "feed_items")
	want := ".items[]? // .data.items[]? // .feed_items[]? // .data.feed_items[]?"
	if got != want {
		t.Errorf("ListRows(items, feed_items) = %q, want %q", got, want)
	}

	// The generic items alias is appended for every noun, exactly once.
	if got := resource.ListRows("memories"); got != ".memories[]? // .data.memories[]? // .items[]? // .data.items[]?" {
		t.Errorf("ListRows(memories) = %q", got)
	}
	if n := strings.Count(resource.ListRows("items"), ".items[]?"); n != 2 {
		t.Errorf("items must not be duplicated, got %d occurrences in %q", n, resource.ListRows("items"))
	}

	// No alternative may be a bare object iteration — the #72 defect.
	for _, n := range nouns {
		if strings.Contains(resource.ListRows(n.keys...), ".data[]?") {
			t.Errorf("%s: bare .data[]? reintroduced: %q", n.name, resource.ListRows(n.keys...))
		}
	}
}
