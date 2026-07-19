package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrNoAuthServer indicates the deployment does not expose an OAuth
// authorization server (discovery endpoint missing) — callers should fall back
// to API-key guidance.
var ErrNoAuthServer = errors.New("no OAuth authorization server on this deployment")

// Metadata is the subset of RFC 8414 authorization-server metadata the flow
// needs.
type Metadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

const (
	discoveryPath  = "/.well-known/oauth-authorization-server"
	resourcePath   = "/.well-known/oauth-protected-resource"
	maxMetadadaLen = 1 << 20 // 1 MiB cap on metadata documents
)

// Discover fetches RFC 8414 authorization-server metadata for baseURL. A 404
// yields ErrNoAuthServer.
func Discover(ctx context.Context, hc *http.Client, baseURL string) (*Metadata, error) {
	url := strings.TrimRight(baseURL, "/") + discoveryPath
	body, status, err := getJSON(ctx, hc, url)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, ErrNoAuthServer
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("discovery %s: unexpected status %d", url, status)
	}
	var m Metadata
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("decode discovery metadata: %w", err)
	}
	if m.AuthorizationEndpoint == "" || m.TokenEndpoint == "" {
		return nil, fmt.Errorf("discovery metadata missing authorization/token endpoint")
	}
	return &m, nil
}

// ProtectedResource is the subset of RFC 9728 protected-resource metadata used
// to derive the RFC 8707 resource indicator.
type ProtectedResource struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// DiscoverResource best-effort fetches the protected-resource identifier to use
// as the RFC 8707 `resource` value. On any failure it returns the trimmed base
// URL, a sensible default.
func DiscoverResource(ctx context.Context, hc *http.Client, baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	body, status, err := getJSON(ctx, hc, trimmed+resourcePath)
	if err != nil || status != http.StatusOK {
		return trimmed
	}
	var pr ProtectedResource
	if err := json.Unmarshal(body, &pr); err != nil || pr.Resource == "" {
		return trimmed
	}
	return pr.Resource
}

// getJSON performs a GET and returns the (size-capped) body and status code.
func getJSON(ctx context.Context, hc *http.Client, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadadaLen))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read %s: %w", url, err)
	}
	return body, resp.StatusCode, nil
}
