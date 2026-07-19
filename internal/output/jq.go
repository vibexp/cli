package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/itchyny/gojq"
	"sigs.k8s.io/yaml"

	"github.com/vibexp/cli/internal/exitcode"
)

// renderJQ applies a gojq expression to the response and writes each result.
// Output is pretty JSON (jq-style, newline-delimited) unless --format=yaml was
// requested, in which case results are YAML documents separated by `---`. A
// bad expression or a jq runtime error is a usage error (exit 2).
func renderJQ(w io.Writer, raw []byte, opts Options) error {
	query, err := gojq.Parse(opts.JQ)
	if err != nil {
		return exitcode.Usage("invalid --jq expression: %v", err)
	}
	var input any
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Errorf("response is not JSON: %w", err)
	}

	asYAML := opts.Format == FormatYAML
	bw := bufio.NewWriter(w)
	iter := query.Run(input)
	count := 0
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if jqErr, ok := v.(error); ok {
			return exitcode.Usage("jq: %v", jqErr)
		}
		if err := writeJQResult(bw, v, asYAML, count); err != nil {
			return err
		}
		count++
	}
	return bw.Flush()
}

func writeJQResult(bw *bufio.Writer, v any, asYAML bool, index int) error {
	if asYAML {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		y, err := yaml.JSONToYAML(b)
		if err != nil {
			return err
		}
		if index > 0 {
			if _, err := bw.WriteString("---\n"); err != nil {
				return err
			}
		}
		_, err = bw.Write(y)
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if _, err := bw.Write(b); err != nil {
		return err
	}
	return bw.WriteByte('\n')
}
