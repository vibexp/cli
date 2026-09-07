package output

import "strings"

// cellReplacer collapses the control whitespace that would otherwise break a
// line-oriented rendering: a tab injects a phantom column into the tabwriter /
// TSV stream, and a newline or carriage return splits one record across several
// lines.
var cellReplacer = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")

// escapeCell makes a data cell safe for the line-oriented renderers. Both
// renderTSV and renderTable go through it so they cannot drift: one record is
// one line, with a stable column count, in both formats.
func escapeCell(s string) string {
	return cellReplacer.Replace(s)
}
