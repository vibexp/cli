package authcmd

import (
	"errors"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/logging"
	"github.com/vibexp/cli/internal/oauth"
)

// preferredScopes are the scopes the CLI would like on the authorization
// request. `mcp` is what VibeXP's embedded authorization server issues, so it
// stays preferred and is still requested against that server. On any deployment
// whose advertised scopes_supported excludes `mcp`, negotiation drops it rather
// than requesting an unknown scope. See oauth.NegotiateScopes.
var preferredScopes = []string{"mcp"}

// runBrowserLogin implements the default `auth login` (interactive OAuth 2.1).
func runBrowserLogin(cmd *cobra.Command, resolve StoreResolver, getenv config.Getenv, scopeOverride []string) error {
	ctx := cmd.Context()
	rt, err := requireRuntime(ctx)
	if err != nil {
		return err
	}
	contextName, err := requireContextName(rt)
	if err != nil {
		return err
	}
	baseURL, err := requireBaseURL(rt)
	if err != nil {
		return err
	}

	// Fail fast where no browser can be opened (servers, SSH sessions).
	if oauth.Headless(getenv) {
		return exitcode.Auth("no browser available for interactive login. Use: vibexp auth login --with-api-key")
	}

	hc := &http.Client{Timeout: rt.Timeout}

	meta, err := oauth.Discover(ctx, hc, baseURL)
	if errors.Is(err, oauth.ErrNoAuthServer) {
		return exitcode.Auth("this deployment has no OAuth server. Use: vibexp auth login --with-api-key")
	}
	if err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}
	resource := oauth.DiscoverResource(ctx, hc, baseURL)

	lis, redirectURI, err := oauth.Listen()
	if err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}

	store, err := resolve()
	if err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}

	// Negotiate the requested scopes against what the server advertises so we
	// never send an unadvertised scope (which some authorization servers reject
	// with invalid_scope). An explicit override (flag > env) short-circuits this.
	// This runs before registration so the scopes can be declared at DCR.
	scopes := oauth.NegotiateScopes(preferredScopes, meta.ScopesSupported, resolveScopeOverride(scopeOverride, getenv))

	// Reuse a previously-registered client only when its declared scopes cover
	// what we will request. An authorization server that grants per-client
	// scopes from the registration bars the client from any scope it did not
	// declare at DCR, so a client registered without the needed scopes (e.g. by
	// an older CLI that declared none) must be re-registered rather than reused.
	clientID := reusableClientID(store, contextName, scopes)
	if clientID == "" {
		clientID, err = oauth.Register(ctx, hc, meta.RegistrationEndpoint, redirectURI, scopes)
		if err != nil {
			return exitcode.New(exitcode.RuntimeErr, err)
		}
	}

	flow := &oauth.Flow{
		HTTPClient:  hc,
		Meta:        meta,
		ClientID:    clientID,
		RedirectURI: redirectURI,
		Resource:    resource,
		Scopes:      scopes,
		Listener:    lis,
		OpenBrowser: browserOpener(cmd),
	}
	cmd.PrintErrln("Opening your browser to sign in… (waiting for authorization)")
	token, err := flow.Run(ctx)
	if err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}
	logging.RegisterSecret(token.AccessToken)
	logging.RegisterSecret(token.RefreshToken)

	// Verify the tokens are accepted by the REST API. A 401 here (with a fresh,
	// valid AS token) means this deployment's REST layer rejects AS JWTs
	// (api_oauth.issuer unset) — guide to API keys instead of a raw 401.
	user, err := fetchIdentity(ctx, baseURL, token.AccessToken, rt.Timeout)
	if err != nil {
		var coded *exitcode.CodedError
		if errors.As(err, &coded) && coded.Code == exitcode.AuthErr {
			return exitcode.Auth("signed in, but this deployment's API does not accept browser-login tokens. Use: vibexp auth login --with-api-key")
		}
		return err
	}

	if err := store.Save(contextName, cred.Entry{
		Type:         cred.TypeOAuth,
		ClientID:     clientID,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.Expiry,
		Scopes:       scopes,
	}); err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}

	cmd.PrintErrf("Logged in to context %q as %s <%s> (browser).\n", contextName, user.Name, string(user.Email))
	return nil
}

// resolveScopeOverride returns an explicit scope override, or nil when none is
// set. Precedence: --scope flag > VIBEXP_OAUTH_SCOPE env > none. The env value
// is split on whitespace and commas so both "a b" and "a,b" work.
func resolveScopeOverride(flagScopes []string, getenv config.Getenv) []string {
	if len(flagScopes) > 0 {
		return flagScopes
	}
	if getenv == nil {
		return nil
	}
	env := getenv("VIBEXP_OAUTH_SCOPE")
	if strings.TrimSpace(env) == "" {
		return nil
	}
	return strings.FieldsFunc(env, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ','
	})
}

// reusableClientID returns the stored OAuth client_id for a context, but only
// when it exists and its recorded scopes cover want; otherwise "" (register a
// fresh client). A stored client whose declared scopes do not cover what we
// need would be barred from those scopes by a scope-enforcing authorization
// server, so it must not be reused.
func reusableClientID(store *cred.Store, contextName string, want []string) string {
	entry, err := store.Get(contextName)
	if err != nil || entry == nil || entry.ClientID == "" {
		return ""
	}
	if !scopesCovered(entry.Scopes, want) {
		return ""
	}
	return entry.ClientID
}

// scopesCovered reports whether every scope in want appears in have.
func scopesCovered(have, want []string) bool {
	if len(want) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(have))
	for _, s := range have {
		set[s] = struct{}{}
	}
	for _, w := range want {
		if _, ok := set[w]; !ok {
			return false
		}
	}
	return true
}

// browserOpener opens the URL and, on failure, prints it for manual opening so
// the flow can still complete.
func browserOpener(cmd *cobra.Command) oauth.BrowserOpener {
	return func(rawURL string) error {
		if err := oauth.OpenBrowser(rawURL); err != nil {
			cmd.PrintErrln("Could not open a browser automatically. Open this URL to continue:")
			cmd.PrintErrln("  " + rawURL)
		}
		return nil
	}
}
