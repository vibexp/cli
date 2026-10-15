package resource

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/output"
)

func TestWriteChannelDetailColumnHeaders(t *testing.T) {
	var headers []string
	for _, c := range WriteChannelDetailColumns() {
		headers = append(headers, c.Header)
	}
	if want := []string{"WRITTEN_VIA", "ASSISTANT"}; !slices.Equal(headers, want) {
		t.Errorf("headers = %v, want %v", headers, want)
	}
}

// TestWriteChannelDetailPaths renders the columns through the real renderer:
// recorded values come through verbatim, while a JSON null (not recorded) and
// an absent field (a pre-v0.16.0 server) both render an empty cell.
func TestWriteChannelDetailPaths(t *testing.T) {
	spec := &output.TableSpec{Rows: ".", Columns: WriteChannelDetailColumns()}
	for _, tc := range []struct {
		name, doc string
		want      []string
	}{
		{"mcp", `{"last_write_source":"mcp","last_write_assistant_name":"Claude Code"}`, []string{"mcp", "Claude Code"}},
		{"web", `{"last_write_source":"web","last_write_assistant_name":null}`, []string{"web", ""}},
		{"null", `{"last_write_source":null,"last_write_assistant_name":null}`, []string{"", ""}},
		{"absent", `{"id":"m-1"}`, []string{"", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := output.Render(&buf, []byte(tc.doc), spec, output.Options{Format: output.FormatText}); err != nil {
				t.Fatal(err)
			}
			if got := strings.Split(strings.TrimRight(buf.String(), "\n"), "\t"); !slices.Equal(got, tc.want) {
				t.Errorf("cells = %q, want %q", got, tc.want)
			}
		})
	}
}
