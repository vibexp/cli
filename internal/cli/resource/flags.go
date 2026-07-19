package resource

import (
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/exitcode"
)

// Pagination holds the shared list pagination flags.
type Pagination struct {
	Limit  int
	Page   int
	Offset int
}

// AddPaginationFlags binds --limit/--page/--offset to a Pagination and returns
// it. A zero value means "unset" (the flag is omitted from the request).
func AddPaginationFlags(cmd *cobra.Command) *Pagination {
	p := &Pagination{}
	cmd.Flags().IntVar(&p.Limit, "limit", 0, "maximum items per page")
	cmd.Flags().IntVar(&p.Page, "page", 0, "page number (1-based)")
	cmd.Flags().IntVar(&p.Offset, "offset", 0, "number of items to skip")
	return p
}

// ApplyToPath merges the set pagination params into a path's query string.
func (p *Pagination) ApplyToPath(path string) (string, error) {
	u, err := url.Parse(path)
	if err != nil {
		return "", exitcode.Usage("invalid path %q: %v", path, err)
	}
	q := u.Query()
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.Offset > 0 {
		q.Set("offset", strconv.Itoa(p.Offset))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
