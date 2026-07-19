package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

// RawClient issues authenticated requests to arbitrary paths against the active
// context, reusing the same transport (timeout/retry/UA) and auth editor as the
// generated client. It backs `vibexp api`.
type RawClient struct {
	baseURL string
	doer    *Doer
	editor  requestEditor
}

// requestEditor mirrors the generated RequestEditorFn signature without
// importing it here.
type requestEditor func(ctx context.Context, req *http.Request) error

// NewRaw builds a RawClient for the resolved runtime.
func NewRaw(ctx context.Context, rt *config.Runtime, credStore *cred.Store, getenv func(string) string) (*RawClient, error) {
	if rt.BaseURL == "" {
		return nil, exitcode.Usage("no base URL for the active context; set one with: vibexp config set-context %s --base-url <url>", contextName(rt))
	}
	editor, err := authEditor(ctx, rt, credStore, getenv)
	if err != nil {
		return nil, err
	}
	return &RawClient{
		baseURL: strings.TrimRight(rt.BaseURL, "/"),
		doer:    NewDoer(rt.Timeout),
		editor:  requestEditor(editor),
	}, nil
}

// BaseURL returns the client's base URL.
func (c *RawClient) BaseURL() string { return c.baseURL }

// Do builds, authenticates, and sends a request. path must be a server-relative
// path (may include a query string); body may be nil. Default headers are set
// first, then caller headers override, then auth is applied last.
func (c *RawClient) Do(ctx context.Context, method, path string, body []byte, headers http.Header) (*http.Response, error) {
	url, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, exitcode.Usage("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vs := range headers {
		req.Header[http.CanonicalHeaderKey(k)] = vs
	}
	if err := c.editor(ctx, req); err != nil {
		return nil, err
	}
	return c.doer.Do(req)
}

// DoStream sends a request whose body is streamed from r with an explicit
// Content-Type (e.g. a multipart boundary type from Stream). Unlike Do it never
// buffers the body into memory, so it is safe for large uploads. The body is not
// replayable; this is used only with non-idempotent methods (POST), which the
// Doer never retries.
func (c *RawClient) DoStream(ctx context.Context, method, path, contentType string, r io.Reader) (*http.Response, error) {
	url, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, exitcode.Usage("build request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	if err := c.editor(ctx, req); err != nil {
		return nil, err
	}
	return c.doer.Do(req)
}

// resolveURL joins a server-relative path to the base URL, rejecting absolute
// URLs and path traversal.
func (c *RawClient) resolveURL(path string) (string, error) {
	if strings.Contains(path, "://") {
		return "", exitcode.Usage("path must be server-relative (e.g. /api/v1/...), not an absolute URL")
	}
	if strings.Contains(path, "..") {
		return "", exitcode.Usage("path must not contain '..'")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.baseURL + path, nil
}

// ReadBody reads and closes a response body (capped) for rendering / error
// mapping.
func ReadBody(resp *http.Response) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return body, nil
}
