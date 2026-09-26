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
	// All walks every page and renders the merged items (see FetchAllPages).
	All bool
}

// LimitHelp is the --limit help shared by every paginated command. The server
// is authoritative on the maximum (it differs per endpoint), so it is
// documented here rather than enforced locally.
const LimitHelp = "maximum items per page (server max 100)"

// AddPaginationFlags binds --limit/--page/--offset/--all to a Pagination and
// returns it. A zero value means "unset" (the flag is omitted from the request).
func AddPaginationFlags(cmd *cobra.Command) *Pagination {
	p := &Pagination{}
	cmd.Flags().IntVar(&p.Limit, "limit", 0, LimitHelp)
	cmd.Flags().IntVar(&p.Page, "page", 0, "page number (1-based)")
	cmd.Flags().IntVar(&p.Offset, "offset", 0, "number of items to skip")
	cmd.Flags().BoolVar(&p.All, "all", false, "fetch every page and output the merged items")
	return p
}

// Validate rejects flag combinations the page walk cannot honour: --all walks
// from page 1 itself, so a fixed --page or --offset contradicts it.
func (p *Pagination) Validate() error {
	if p.All && p.Page > 0 {
		return exitcode.Usage("--all and --page are mutually exclusive")
	}
	if p.All && p.Offset > 0 {
		return exitcode.Usage("--all and --offset are mutually exclusive")
	}
	return nil
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
