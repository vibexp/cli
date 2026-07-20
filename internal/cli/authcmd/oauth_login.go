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

	// Reuse a previously-registered client_id for this context when present.
	clientID := existingClientID(store, contextName)
	if clientID == "" {
		clientID, err = oauth.Register(ctx, hc, meta.RegistrationEndpoint, redirectURI)
		if err != nil {
			return exitcode.New(exitcode.RuntimeErr, err)
		}
	}

	// Negotiate the requested scopes against what the server advertises so we
	// never send an unadvertised scope (which some authorization servers reject
	// with invalid_scope). An explicit override (flag > env) short-circuits this.
	scopes := oauth.NegotiateScopes(preferredScopes, meta.ScopesSupported, resolveScopeOverride(scopeOverride, getenv))

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

// existingClientID returns the stored OAuth client_id for a context, or "".
func existingClientID(store *cred.Store, contextName string) string {
	entry, err := store.Get(contextName)
	if err != nil || entry == nil {
		return ""
	}
	return entry.ClientID
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
