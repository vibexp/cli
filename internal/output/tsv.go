package output

import (
	"bufio"
	"io"
	"strings"
)

// renderTSV writes tab-separated rows with no header and no color — the
// machine-friendly form used when stdout is piped. Cell tabs/newlines are
// escaped so each record stays one line with a stable column count.
func renderTSV(w io.Writer, raw []byte, spec *TableSpec) error {
	rows, err := extractRows(raw, spec)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	for _, r := range rows {
		for i, cell := range r {
			r[i] = escapeTSV(cell)
		}
		if _, err := bw.WriteString(strings.Join(r, "\t") + "\n"); err != nil {
			return err
		}
	}
	return bw.Flush()
}

func escapeTSV(s string) string {
	replacer := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	return replacer.Replace(s)
}
