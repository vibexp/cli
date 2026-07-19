package oauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultCallbackTimeout bounds how long the CLI waits for the browser leg.
const defaultCallbackTimeout = 3 * time.Minute

// BrowserOpener opens a URL in the user's browser. Injectable for tests and for
// the login command's manual-URL fallback.
type BrowserOpener func(rawURL string) error

// Listen binds a single-use loopback callback listener on a random port and
// returns it along with the redirect URI to register and authorize against.
func Listen() (net.Listener, string, error) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("bind loopback callback listener: %w", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	return lis, redirectURI, nil
}

// Flow runs the authorization-code + PKCE leg over a pre-bound loopback
// listener.
type Flow struct {
	HTTPClient  *http.Client
	Meta        *Metadata
	ClientID    string
	RedirectURI string
	Resource    string
	Scopes      []string
	OpenBrowser BrowserOpener
	Listener    net.Listener
	Timeout     time.Duration
}

type callbackResult struct {
	code string
	err  error
}

// Run drives the browser authorization and exchanges the returned code for
// tokens. It always closes the listener.
func (f *Flow) Run(ctx context.Context) (*Token, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}
	state, err := GenerateState()
	if err != nil {
		return nil, err
	}

	authURL := f.authorizeURL(pkce, state)

	results := make(chan callbackResult, 1)
	srv := &http.Server{
		Handler:           callbackHandler(state, results),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(f.Listener) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	if err := f.OpenBrowser(authURL); err != nil {
		return nil, fmt.Errorf("open browser: %w", err)
	}

	timeout := f.Timeout
	if timeout <= 0 {
		timeout = defaultCallbackTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case <-waitCtx.Done():
		return nil, fmt.Errorf("timed out waiting for browser authorization: %w", waitCtx.Err())
	case res := <-results:
		if res.err != nil {
			return nil, res.err
		}
		return ExchangeCode(ctx, f.HTTPClient, f.Meta.TokenEndpoint, f.ClientID,
			res.code, pkce.Verifier, f.RedirectURI, f.Resource)
	}
}

// authorizeURL builds the RFC 6749 / RFC 8707 authorization request URL.
func (f *Flow) authorizeURL(pkce PKCE, state string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {f.ClientID},
		"redirect_uri":          {f.RedirectURI},
		"state":                 {state},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {pkce.Method},
	}
	if len(f.Scopes) > 0 {
		q.Set("scope", strings.Join(f.Scopes, " "))
	}
	if f.Resource != "" {
		q.Set("resource", f.Resource)
	}
	sep := "?"
	if strings.Contains(f.Meta.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	return f.Meta.AuthorizationEndpoint + sep + q.Encode()
}

// callbackHandler returns the single-use loopback handler that captures the
// authorization result.
func callbackHandler(wantState string, results chan<- callbackResult) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			finish(w, results, "", fmt.Errorf("authorization failed: %s %s", e, q.Get("error_description")),
				"Authorization failed. You may close this window and return to the terminal.")
			return
		}
		if q.Get("state") != wantState {
			finish(w, results, "", errors.New("state mismatch on callback (possible CSRF)"),
				"Authorization could not be verified. You may close this window.")
			return
		}
		code := q.Get("code")
		if code == "" {
			finish(w, results, "", errors.New("no authorization code in callback"),
				"Authorization failed. You may close this window.")
			return
		}
		finish(w, results, code, nil,
			"Login successful. You may close this window and return to the terminal.")
	})
	return mux
}

// finish writes a minimal HTML page and delivers the result exactly once.
func finish(w http.ResponseWriter, results chan<- callbackResult, code string, err error, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html><html><body style=\"font-family:sans-serif\"><p>%s</p></body></html>", msg)
	select {
	case results <- callbackResult{code: code, err: err}:
	default: // a result was already delivered; ignore duplicate hits
	}
}
