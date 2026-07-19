package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// ANSI bold for table headers (only emitted when color is enabled).
const (
	ansiBold  = "\x1b[1m"
	ansiReset = "\x1b[0m"
)

// renderTable writes an aligned, optionally-colored table with a header row.
func renderTable(w io.Writer, raw []byte, spec *TableSpec, color bool) error {
	rows, err := extractRows(raw, spec)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	headers := make([]string, len(spec.Columns))
	for i, c := range spec.Columns {
		h := c.Header
		if color {
			h = ansiBold + h + ansiReset
		}
		headers[i] = h
	}
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	return tw.Flush()
}
