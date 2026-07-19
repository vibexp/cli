package apicmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

var errRuntimeMissing = errors.New("internal: runtime not initialized")

// defaultPageLimit is the per-page size used when the path sets none.
const defaultPageLimit = 50

// runPaginate walks a page/offset-based list endpoint, incrementing `page`
// until a short/empty page, and renders the union of all items as one JSON
// array. If the first page has no recognizable list field, it renders that page
// raw and stops (documented fallback).
func runPaginate(ctx context.Context, cmd *cobra.Command, client *api.RawClient, method, path string, hdr http.Header, rt *config.Runtime, getenv config.Getenv) error {
	if method != http.MethodGet {
		return exitcode.Usage("--paginate only supports GET requests")
	}
	u, err := url.Parse(path)
	if err != nil {
		return exitcode.Usage("invalid path %q: %v", path, err)
	}
	q := u.Query()

	limit := defaultPageLimit
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	} else {
		q.Set("limit", strconv.Itoa(limit))
	}
	startPage := 1
	if p := q.Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			startPage = n
		}
	}

	var items []json.RawMessage
	for page := startPage; ; page++ {
		q.Set("page", strconv.Itoa(page))
		u.RawQuery = q.Encode()

		resp, err := client.Do(ctx, method, u.String(), nil, hdr)
		if err != nil {
			return err
		}
		raw, err := api.ReadBody(resp)
		if err != nil {
			return exitcode.New(exitcode.RuntimeErr, err)
		}
		if cerr := api.Check(resp.StatusCode, raw); cerr != nil {
			return cerr
		}

		pageItems, ok := extractItems(raw)
		if !ok {
			if page == startPage {
				// Not a recognizable list — stream the single page as-is.
				return renderBody(cmd, rt, getenv, raw)
			}
			break
		}
		items = append(items, pageItems...)
		// Stop on a short/empty page, or once the response's own total_pages
		// says we're done (guards against an endpoint that always returns a
		// full page and would otherwise loop forever).
		if len(pageItems) < limit {
			break
		}
		if total, ok := readTotalPages(raw); ok && page >= total {
			break
		}
	}

	if items == nil {
		items = []json.RawMessage{}
	}
	merged, err := json.Marshal(items)
	if err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}
	return renderBody(cmd, rt, getenv, merged)
}

// readTotalPages returns the response's total_pages metadata when present.
func readTotalPages(raw []byte) (int, bool) {
	var meta struct {
		TotalPages *int `json:"total_pages"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil || meta.TotalPages == nil {
		return 0, false
	}
	return *meta.TotalPages, true
}

// extractItems returns the list items in a page response: a top-level array, or
// an object's `items`/`data` field, or (deterministically) its first
// array-valued field.
func extractItems(raw []byte) ([]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []json.RawMessage
		if json.Unmarshal(trimmed, &arr) == nil {
			return arr, true
		}
		return nil, false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil, false
	}
	for _, key := range []string{"items", "data"} {
		if v, ok := obj[key]; ok {
			var arr []json.RawMessage
			if json.Unmarshal(v, &arr) == nil {
				return arr, true
			}
		}
	}
	// Deterministic fallback: first array-valued key by name.
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var arr []json.RawMessage
		if json.Unmarshal(obj[k], &arr) == nil {
			return arr, true
		}
	}
	return nil, false
}
