package output

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/itchyny/gojq"

	"github.com/vibexp/cli/internal/exitcode"
)

// compiledSpec holds a TableSpec's parsed gojq queries.
type compiledSpec struct {
	rows    *gojq.Query
	columns []*gojq.Query
}

func compileSpec(spec *TableSpec) (*compiledSpec, error) {
	rowsExpr := spec.Rows
	if rowsExpr == "" {
		rowsExpr = "."
	}
	rows, err := gojq.Parse(rowsExpr)
	if err != nil {
		return nil, exitcode.Usage("invalid table rows expression %q: %v", rowsExpr, err)
	}
	cols := make([]*gojq.Query, len(spec.Columns))
	for i, c := range spec.Columns {
		q, err := gojq.Parse(c.Path)
		if err != nil {
			return nil, exitcode.Usage("invalid column path %q: %v", c.Path, err)
		}
		cols[i] = q
	}
	return &compiledSpec{rows: rows, columns: cols}, nil
}

// extractRows applies the compiled spec to the raw JSON, returning one string
// slice per row.
func extractRows(raw []byte, spec *TableSpec) ([][]string, error) {
	cs, err := compileSpec(spec)
	if err != nil {
		return nil, err
	}
	var input any
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, fmt.Errorf("response is not JSON: %w", err)
	}

	var out [][]string
	iter := cs.rows.Run(input)
	for {
		rowVal, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := rowVal.(error); ok {
			return nil, fmt.Errorf("extract rows: %w", err)
		}
		row := make([]string, len(cs.columns))
		for i, q := range cs.columns {
			row[i] = firstString(q.Run(rowVal))
		}
		out = append(out, row)
	}
	return out, nil
}

// firstString runs an iterator and stringifies its first result.
func firstString(it gojq.Iter) string {
	v, ok := it.Next()
	if !ok {
		return ""
	}
	if _, isErr := v.(error); isErr {
		return ""
	}
	return stringify(v)
}

// stringify renders a scalar for a table cell; composite values become compact
// JSON.
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	}
}
