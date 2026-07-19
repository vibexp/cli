package output

import (
	"fmt"
	"io"

	"github.com/vibexp/cli/internal/exitcode"
)

// Format is an output format selector.
type Format string

const (
	// FormatAuto picks table on a TTY and TSV when piped.
	FormatAuto  Format = ""
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
	FormatTable Format = "table"
	FormatText  Format = "text"
)

// ParseFormat validates a --format / VIBEXP_FORMAT value.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatAuto, FormatJSON, FormatYAML, FormatTable, FormatText:
		return Format(s), nil
	default:
		return "", exitcode.Usage("invalid --format %q: want json|yaml|table|text", s)
	}
}

// Column is one table/TSV column: a header and a gojq path applied to each row.
type Column struct {
	Header string
	Path   string
}

// TableSpec declares how to tabulate a response: Rows is a gojq expression
// yielding the stream of row values (default "." — the whole document as one
// row), and Columns extract cells from each row.
type TableSpec struct {
	Rows    string
	Columns []Column
}

// Options controls a render.
type Options struct {
	Format Format
	IsTTY  bool
	Color  bool
	JQ     string
}

// Render writes raw (the exact API response body) to w per opts. A --jq filter,
// when present, is applied first and forces JSON output (or YAML if explicitly
// requested). Table/text formats require a spec; without one they fall back to
// the raw JSON body, which keeps raw passthrough (e.g. `vibexp api`) correct.
func Render(w io.Writer, raw []byte, spec *TableSpec, opts Options) error {
	if opts.JQ != "" {
		return renderJQ(w, raw, opts)
	}

	format := opts.Format
	if format == FormatAuto {
		if opts.IsTTY {
			format = FormatTable
		} else {
			format = FormatText
		}
	}

	switch format {
	case FormatJSON:
		return renderJSON(w, raw)
	case FormatYAML:
		return renderYAML(w, raw)
	case FormatTable:
		if spec == nil {
			return renderJSON(w, raw)
		}
		return renderTable(w, raw, spec, opts.Color)
	case FormatText:
		if spec == nil {
			return renderJSON(w, raw)
		}
		return renderTSV(w, raw, spec)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}
