package oauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// defaultCallbackTimeout bounds how long the CLI waits for the browser leg.
const defaultCallbackTimeout = 3 * time.Minute

// BrowserOpener opens a URL in the user's browser. Injectable for tests and for
// the login command's manual-URL fallback.
type BrowserOpener func(rawURL string) error

// AuthzError is an RFC 6749 §4.1.2.1 error delivered on the authorization
// callback (`?error=…&error_description=…`). Callers branch on Code; the
// message keeps the historical wording.
type AuthzError struct {
	Code        string
	Description string
}

func (e *AuthzError) Error() string {
	return fmt.Sprintf("authorization failed: %s %s", e.Code, e.Description)
}

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

	// Notify, when set, receives short user-facing notices — currently only the
	// warning before the no-scope retry reopens the browser. Nil is a no-op, so
	// the package stays usable without a printer.
	Notify func(string)

	// UsedScopes are the scopes on the authorization request that succeeded:
	// Scopes normally, nil when the no-scope retry is what worked. Only
	// meaningful after Run returns without error; callers persist this rather
	// than Scopes so the next login reasons about what the server accepted.
	UsedScopes []string

	// cur is the in-flight attempt. The callback server outlives an individual
	// attempt, so the handler reads the state nonce and result channel from
	// here rather than closing over one attempt's values.
	cur atomic.Pointer[attempt]
}

// attempt is the per-authorization-request state the long-lived callback
// handler needs.
type attempt struct {
	state   string
	results chan callbackResult
}

type callbackResult struct {
	code string
	err  error
}

// Run drives the browser authorization and exchanges the returned code for
// tokens. It always closes the listener.
//
// An `invalid_scope` callback gets exactly one retry with the scope parameter
// omitted, letting the authorization server apply its own default grant. The
// retry reuses the same listener, port, redirect URI and client_id — the
// redirect URI is pinned by the dynamic client registration, so re-binding
// would mean re-registering — but generates a fresh state nonce and PKCE pair.
func (f *Flow) Run(ctx context.Context) (*Token, error) {
	srv := &http.Server{
		Handler:           f.callbackHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(f.Listener) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	tok, err := f.runOnce(ctx, f.Scopes)
	if err == nil {
		f.UsedScopes = f.Scopes
		return tok, nil
	}

	// Retry only the one failure a retry can fix, and only when there is a
	// scope to drop.
	var authz *AuthzError
	if !errors.As(err, &authz) || authz.Code != "invalid_scope" || len(f.Scopes) == 0 {
		return nil, err
	}

	f.notify(fmt.Sprintf("The authorization server rejected the requested scope %q; retrying without it — a second browser window will open.",
		strings.Join(f.Scopes, " ")))

	tok, err = f.runOnce(ctx, nil)
	if err != nil {
		return nil, err
	}
	f.UsedScopes = nil
	return tok, nil
}

// runOnce performs a single authorization attempt against an already-running
// callback server: fresh PKCE and state, browser open, wait, code exchange.
func (f *Flow) runOnce(ctx context.Context, scopes []string) (*Token, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}
	state, err := GenerateState()
	if err != nil {
		return nil, err
	}

	results := make(chan callbackResult, 1)
	f.cur.Store(&attempt{state: state, results: results})

	if err := f.OpenBrowser(f.authorizeURL(pkce, state, scopes)); err != nil {
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

// notify delivers a user-facing notice when a printer is wired up.
func (f *Flow) notify(msg string) {
	if f.Notify != nil {
		f.Notify(msg)
	}
}

// authorizeURL builds the RFC 6749 / RFC 8707 authorization request URL. An
// empty scopes slice omits the scope parameter entirely.
func (f *Flow) authorizeURL(pkce PKCE, state string, scopes []string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {f.ClientID},
		"redirect_uri":          {f.RedirectURI},
		"state":                 {state},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {pkce.Method},
	}
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
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

// callbackHandler returns the loopback handler that captures the authorization
// result of whichever attempt is currently in flight.
func (f *Flow) callbackHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		cur := f.cur.Load()
		if cur == nil { // no attempt in flight; nothing to resolve
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		// A late callback from a superseded attempt carries that attempt's
		// state and must not resolve this one. Only enforced when the server
		// echoed a state: not every server echoes it on the error path, and
		// hiding a real error behind "state mismatch" would be worse.
		if s := q.Get("state"); s != "" && s != cur.state {
			finish(w, cur.results, "", errors.New("state mismatch on callback (possible CSRF)"),
				"Authorization could not be verified. You may close this window.")
			return
		}
		if e := q.Get("error"); e != "" {
			finish(w, cur.results, "", &AuthzError{Code: e, Description: q.Get("error_description")},
				"Authorization failed. You may close this window and return to the terminal.")
			return
		}
		if q.Get("state") != cur.state {
			finish(w, cur.results, "", errors.New("state mismatch on callback (possible CSRF)"),
				"Authorization could not be verified. You may close this window.")
			return
		}
		code := q.Get("code")
		if code == "" {
			finish(w, cur.results, "", errors.New("no authorization code in callback"),
				"Authorization failed. You may close this window.")
			return
		}
		finish(w, cur.results, code, nil,
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
