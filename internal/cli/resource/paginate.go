package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/exitcode"
)

// DefaultPageLimit is the per-page size a page walk uses when the path sets
// none.
const DefaultPageLimit = 50

// FetchAllPages walks a page-based GET list endpoint, incrementing `page` until
// a short/empty page or the response's own total_pages, and returns the union
// of every page's items in order. It is the one page walk behind both
// `vibexp api --paginate` and the resource `list --all` flag.
//
// When the first page has no recognizable list field, items is nil and
// notList carries that page's raw body, so the caller can render it as-is.
func FetchAllPages(ctx context.Context, client *api.RawClient, path string, hdr http.Header) (items []json.RawMessage, notList []byte, err error) {
	u, err := url.Parse(path)
	if err != nil {
		return nil, nil, exitcode.Usage("invalid path %q: %v", path, err)
	}
	q := u.Query()

	limit := DefaultPageLimit
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

	items = []json.RawMessage{}
	for page := startPage; ; page++ {
		q.Set("page", strconv.Itoa(page))
		u.RawQuery = q.Encode()

		resp, err := client.Do(ctx, http.MethodGet, u.String(), nil, hdr)
		if err != nil {
			return nil, nil, err
		}
		raw, err := api.ReadBody(resp)
		if err != nil {
			return nil, nil, exitcode.New(exitcode.RuntimeErr, err)
		}
		if cerr := api.Check(resp.StatusCode, raw); cerr != nil {
			return nil, nil, cerr
		}

		pageItems, ok := ExtractItems(raw)
		if !ok {
			if page == startPage {
				// Not a recognizable list — hand the single page back as-is.
				return nil, raw, nil
			}
			break
		}
		items = append(items, pageItems...)
		if lastPage(raw, page, len(pageItems), limit) {
			break
		}
	}
	return items, nil, nil
}

// lastPage reports whether the walk must stop after this page. Besides a
// short/empty page and the response's own total_pages, it stops when the
// endpoint evidently does not paginate — it reports no page metadata at all,
// returned more than the limit, or answered a different page than the one
// asked for (e.g. GET /attachments, which ignores page/limit and returns every
// item on every request). Without those guards such an endpoint loops forever.
//
// The page size compared against is the server's own per_page when reported,
// so a server that clamps an oversized limit is still walked to the end.
func lastPage(raw []byte, page, got, limit int) bool {
	m, ok := readMeta(raw)
	if ok && m.PerPage != nil && *m.PerPage > 0 {
		limit = *m.PerPage
	}
	if got != limit {
		return true
	}
	if !ok || (m.Page == nil && m.TotalPages == nil) {
		return true
	}
	if m.Page != nil && *m.Page != page {
		return true
	}
	return m.TotalPages != nil && *m.TotalPages > 0 && page >= *m.TotalPages
}

// pageMeta is the raw pagination metadata of a list response; nil = absent.
type pageMeta struct {
	Page       *int `json:"page"`
	PerPage    *int `json:"per_page"`
	TotalPages *int `json:"total_pages"`
	TotalCount *int `json:"total_count"`
}

// readMeta reads a response's pagination metadata from the top level or, for
// an enveloped list (listPrompts: {"data":{"prompts":[…],"page":…}}), from
// under an object-valued `data`.
func readMeta(raw []byte) (pageMeta, bool) {
	var top struct {
		pageMeta
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &top) != nil {
		return pageMeta{}, false
	}
	m := top.pageMeta
	if m.Page == nil && m.PerPage == nil && m.TotalPages == nil && len(top.Data) > 0 {
		var inner pageMeta
		if json.Unmarshal(top.Data, &inner) == nil {
			m = inner
		}
	}
	return m, true
}

// PageMeta is the pagination metadata a list response carries.
type PageMeta struct {
	Page       int
	TotalPages int
	// TotalCount is -1 when the response does not report it.
	TotalCount int
}

// ReadPageMeta returns a response's page/total_pages/total_count metadata, read
// from the top level or, for an enveloped list (listPrompts:
// {"data":{"prompts":[…],"page":…}}), from under `data`. ok is false when the
// response carries no total_pages.
func ReadPageMeta(raw []byte) (PageMeta, bool) {
	m, ok := readMeta(raw)
	if !ok || m.TotalPages == nil {
		return PageMeta{}, false
	}
	out := PageMeta{Page: 1, TotalPages: *m.TotalPages, TotalCount: -1}
	if m.Page != nil {
		out.Page = *m.Page
	}
	if m.TotalCount != nil {
		out.TotalCount = *m.TotalCount
	}
	return out, true
}

// WarnIfPartial writes a one-line stderr hint when a list response is only one
// page of several, so a silently partial result becomes visible. stdout is
// never touched. how names the flags that reach the rest (e.g.
// "--page, --limit (max 100) or --all"). Nothing is written when everything fit
// or the response carries no page metadata.
func WarnIfPartial(w io.Writer, raw []byte, how string) {
	meta, ok := ReadPageMeta(raw)
	if !ok || meta.TotalPages <= meta.Page {
		return
	}
	count := ""
	if items, ok := ExtractItems(raw); ok {
		if meta.TotalCount >= 0 {
			count = fmt.Sprintf(" (%d of %d)", len(items), meta.TotalCount)
		} else {
			count = fmt.Sprintf(" (%d items)", len(items))
		}
	}
	_, _ = fmt.Fprintf(w, "showing page %d of %d%s; use %s\n", meta.Page, meta.TotalPages, count, how)
}

// UsageOnBadRequest turns a 400 from a list/search request into a usage error
// (exit 2): the request is built from the user's flags (--limit, --page,
// filters), so the server rejecting it is a usage problem. The server's own
// message and request_id are kept verbatim. Any other error passes through.
func UsageOnBadRequest(err error) error {
	var apiErr *api.Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest {
		return exitcode.New(exitcode.UsageErr, err)
	}
	return err
}

// ExtractItems returns the list items in a page response: a top-level array, or
// an object's `items`/`data` field, or (deterministically) its first
// array-valued field. An object-valued `data` envelope with no array beside it
// (listPrompts) is searched the same way one level down.
func ExtractItems(raw []byte) ([]json.RawMessage, bool) {
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
	if arr, ok := arrayField(obj); ok {
		return arr, true
	}
	if data, ok := obj["data"]; ok {
		var inner map[string]json.RawMessage
		if json.Unmarshal(data, &inner) == nil {
			return arrayField(inner)
		}
	}
	return nil, false
}

// arrayField returns obj's `items`/`data` array, else its first array-valued
// field by name.
func arrayField(obj map[string]json.RawMessage) ([]json.RawMessage, bool) {
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
