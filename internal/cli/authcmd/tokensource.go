package authcmd

import (
	"context"
	"errors"
	"net/http"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/oauth"
)

// freshBearer returns a usable bearer token for the resolved credential,
// transparently refreshing an expired OAuth token (under the cross-process
// lock) via the authorization server. Env and API-key credentials are returned
// as-is. An unrenewable OAuth session maps to exit 4 with a re-login prompt.
func freshBearer(ctx context.Context, store *cred.Store, rt *config.Runtime, resolved cred.Resolved) (string, error) {
	if resolved.Source != cred.SourceStored || resolved.Type != cred.TypeOAuth {
		return resolved.Bearer, nil
	}

	entry, err := store.Get(rt.ContextName)
	if err != nil {
		return "", exitcode.New(exitcode.RuntimeErr, err)
	}
	if entry == nil {
		return "", exitcode.Auth("%s", oauth.ErrReauthRequired.Error())
	}

	hc := &http.Client{Timeout: rt.Timeout}
	meta, err := oauth.Discover(ctx, hc, rt.BaseURL)
	if err != nil {
		return "", exitcode.New(exitcode.RuntimeErr, err)
	}
	refresher := &oauth.Refresher{
		HTTPClient:    hc,
		Store:         store,
		ContextName:   rt.ContextName,
		TokenEndpoint: meta.TokenEndpoint,
		ClientID:      entry.ClientID,
		Resource:      oauth.DiscoverResource(ctx, hc, rt.BaseURL),
	}
	token, err := refresher.AccessToken(ctx)
	if errors.Is(err, oauth.ErrReauthRequired) {
		return "", exitcode.Auth("%s", oauth.ErrReauthRequired.Error())
	}
	if err != nil {
		return "", exitcode.New(exitcode.RuntimeErr, err)
	}
	return token, nil
}

// methodLabel renders a human description of the auth method for `auth status`.
func methodLabel(resolved cred.Resolved) string {
	switch {
	case resolved.Source == cred.SourceEnv:
		return "API key (environment " + cred.EnvAPIKey + ")"
	case resolved.Type == cred.TypeOAuth:
		return "browser login (OAuth 2.1)"
	default:
		return "API key (stored)"
	}
}
