package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// clientName is the DCR client_name advertised for this CLI.
const clientName = "vibexp-cli"

// registrationRequest is the RFC 7591 dynamic client registration payload for a
// public client using a loopback redirect.
type registrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

type registrationResponse struct {
	ClientID string `json:"client_id"`
}

// Register performs RFC 7591 dynamic client registration for a public client
// and returns the issued client_id.
func Register(ctx context.Context, hc *http.Client, registrationEndpoint, redirectURI string) (string, error) {
	if registrationEndpoint == "" {
		return "", fmt.Errorf("deployment does not support dynamic client registration")
	}
	payload := registrationRequest{
		ClientName:              clientName,
		RedirectURIs:            []string{redirectURI},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none", // public client
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal DCR request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("register client: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxMetadadaLen))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("dynamic client registration failed: status %d", resp.StatusCode)
	}
	var out registrationResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode DCR response: %w", err)
	}
	if out.ClientID == "" {
		return "", fmt.Errorf("dynamic client registration returned no client_id")
	}
	return out.ClientID, nil
}
