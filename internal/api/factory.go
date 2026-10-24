package api

import (
	"context"
	"net/http"
	"sync"

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
		return nil, MissingBaseURLError(rt)
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
	d, err := discover(ctx, hc, rt.BaseURL)
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
		TokenEndpoint: d.tokenEndpoint,
		ClientID:      clientID,
		Resource:      d.resource,
	}, nil
}

// discovered is the OAuth discovery a refresher needs for one base URL.
type discovered struct {
	tokenEndpoint string
	resource      string
}

// discoveryCacheKey keys the per-invocation discovery cache on a context.
type discoveryCacheKey struct{}

// discoveryCache memoizes successful discovery by base URL.
type discoveryCache struct {
	mu      sync.Mutex
	entries map[string]discovered
}

// WithDiscoveryCache returns a copy of ctx carrying an OAuth discovery cache,
// so every client built under it — the command's own and the slug resolver's
// — discovers a base URL once. The root installs it for each invocation.
func WithDiscoveryCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, discoveryCacheKey{}, &discoveryCache{entries: map[string]discovered{}})
}

// discover runs RFC 8414 + RFC 9728 discovery for baseURL, through the
// context's cache when there is one. A failure is never cached.
func discover(ctx context.Context, hc *http.Client, baseURL string) (discovered, error) {
	cache, _ := ctx.Value(discoveryCacheKey{}).(*discoveryCache)
	if cache != nil {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		if d, ok := cache.entries[baseURL]; ok {
			return d, nil
		}
	}
	meta, err := oauth.Discover(ctx, hc, baseURL)
	if err != nil {
		return discovered{}, err
	}
	d := discovered{tokenEndpoint: meta.TokenEndpoint, resource: oauth.DiscoverResource(ctx, hc, baseURL)}
	if cache != nil {
		cache.entries[baseURL] = d
	}
	return d, nil
}
