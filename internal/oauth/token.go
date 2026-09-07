package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrInvalidGrant is returned when the authorization server rejects a refresh
// token (expired, revoked, or a rotation-reuse detection). Callers should wipe
// the stored session and prompt re-login.
var ErrInvalidGrant = errors.New("refresh token rejected (expired or already used)")

// Token is a resolved token set with a computed absolute expiry.
type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Expiry       time.Time
	// Scopes are the scopes the authorization server actually GRANTED, from the
	// RFC 6749 §5.1 `scope` response parameter — not the ones requested. When
	// the response omits it, §5.1 defines that as "identical to the request", so
	// this falls back to the scopes that request carried. Callers persist this
	// rather than what they asked for, because a server may narrow the grant.
	Scopes []string
}

// Valid reports whether the access token is present and not within leeway of
// expiry at the given time. A zero Expiry is treated as non-expiring.
func (t *Token) Valid(now time.Time, leeway time.Duration) bool {
	if t.AccessToken == "" {
		return false
	}
	if t.Expiry.IsZero() {
		return true
	}
	return now.Add(leeway).Before(t.Expiry)
}

// tokenResponse is the RFC 6749 token endpoint success body.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// tokenErrorResponse is the RFC 6749 token endpoint error body.
type tokenErrorResponse struct {
	ErrorCode        string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ExchangeCode swaps an authorization code for tokens (with PKCE verifier and
// RFC 8707 resource indicator). requestedScopes are the scopes the
// authorization request carried; they are the RFC 6749 §5.1 fallback for
// Token.Scopes when the token response omits `scope`.
func ExchangeCode(ctx context.Context, hc *http.Client, tokenEndpoint, clientID, code, verifier, redirectURI, resource string, requestedScopes []string) (*Token, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}
	if resource != "" {
		form.Set("resource", resource)
	}
	return postToken(ctx, hc, tokenEndpoint, form, requestedScopes)
}

// Refresh exchanges a refresh token for a new token set. A rotated server
// returns a new refresh token which callers must persist.
func Refresh(ctx context.Context, hc *http.Client, tokenEndpoint, clientID, refreshToken, resource string) (*Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}
	if resource != "" {
		form.Set("resource", resource)
	}
	// A refresh request carries no scope of its own, so there is nothing for an
	// omitted response `scope` to fall back to: Token.Scopes is then nil, and
	// the caller keeps whatever the authorization grant recorded.
	return postToken(ctx, hc, tokenEndpoint, form, nil)
}

// postToken performs the token endpoint POST and maps the response.
// requestedScopes is the RFC 6749 §5.1 fallback for an omitted `scope`.
func postToken(ctx context.Context, hc *http.Client, tokenEndpoint string, form url.Values, requestedScopes []string) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxMetadadaLen))

	if resp.StatusCode != http.StatusOK {
		var terr tokenErrorResponse
		_ = json.Unmarshal(body, &terr)
		if terr.ErrorCode == "invalid_grant" {
			return nil, ErrInvalidGrant
		}
		if terr.ErrorCode != "" {
			return nil, fmt.Errorf("token endpoint error %q: %s", terr.ErrorCode, terr.ErrorDescription)
		}
		return nil, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}
	tok := &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
		Scopes:       requestedScopes,
	}
	// A present `scope` is authoritative and may narrow the request; an absent
	// one means "identical to the request", which is the fallback above.
	if strings.TrimSpace(tr.Scope) != "" {
		tok.Scopes = strings.Fields(tr.Scope)
	}
	if tr.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return tok, nil
}
