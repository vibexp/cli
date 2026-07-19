package api

import (
	"context"
	"net/http"

	vibexp "github.com/vibexp/api-client-go"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/logging"
	"github.com/vibexp/cli/internal/oauth"
)

// New returns a ready authenticated client for the resolved runtime: base URL,
// the retry/timeout transport, and an auth request editor sourced from the
// credential store (API key, env, or a transparently-refreshed OAuth token).
func New(ctx context.Context, rt *config.Runtime, credStore *cred.Store, getenv func(string) string) (*vibexp.ClientWithResponses, error) {
	if rt.BaseURL == "" {
		return nil, exitcode.Usage("no base URL for the active context; set one with: vibexp config set-context %s --base-url <url>", contextName(rt))
	}
	editor, err := authEditor(ctx, rt, credStore, getenv)
	if err != nil {
		return nil, err
	}
	return vibexp.NewClientWithResponses(rt.BaseURL,
		vibexp.WithHTTPClient(NewDoer(rt.Timeout)),
		vibexp.WithRequestEditorFn(editor),
	)
}

// authEditor builds the RequestEditorFn that sets the Authorization header,
// resolving the bearer per the credential precedence and refreshing OAuth
// tokens on the fly.
func authEditor(ctx context.Context, rt *config.Runtime, credStore *cred.Store, getenv func(string) string) (vibexp.RequestEditorFn, error) {
	resolved, err := credStore.Resolve(rt.ContextName, getenv)
	if err != nil {
		return nil, exitcode.New(exitcode.RuntimeErr, err)
	}
	if resolved.Source == cred.SourceNone {
		return nil, exitcode.Auth("not authenticated for context %q; run: vibexp auth login", rt.ContextName)
	}

	// Stored OAuth: refresh transparently on each request (cheap when valid).
	if resolved.Source == cred.SourceStored && resolved.Type == cred.TypeOAuth {
		refresher, err := oauthRefresher(ctx, rt, credStore)
		if err != nil {
			return nil, err
		}
		return func(reqCtx context.Context, req *http.Request) error {
			tok, err := refresher.AccessToken(reqCtx)
			if err != nil {
				return err
			}
			logging.RegisterSecret(tok)
			req.Header.Set("Authorization", "Bearer "+tok)
			return nil
		}, nil
	}

	// API key (stored or from VIBEXP_API_KEY).
	bearer := resolved.Bearer
	logging.RegisterSecret(bearer)
	return func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+bearer)
		return nil
	}, nil
}

// oauthRefresher wires an OAuth token refresher for the context (discovery +
// stored client_id).
func oauthRefresher(ctx context.Context, rt *config.Runtime, credStore *cred.Store) (*oauth.Refresher, error) {
	hc := &http.Client{Timeout: rt.Timeout}
	meta, err := oauth.Discover(ctx, hc, rt.BaseURL)
	if err != nil {
		return nil, exitcode.New(exitcode.RuntimeErr, err)
	}
	entry, err := credStore.Get(rt.ContextName)
	if err != nil {
		return nil, exitcode.New(exitcode.RuntimeErr, err)
	}
	clientID := ""
	if entry != nil {
		clientID = entry.ClientID
	}
	return &oauth.Refresher{
		HTTPClient:    hc,
		Store:         credStore,
		ContextName:   rt.ContextName,
		TokenEndpoint: meta.TokenEndpoint,
		ClientID:      clientID,
		Resource:      oauth.DiscoverResource(ctx, hc, rt.BaseURL),
	}, nil
}
