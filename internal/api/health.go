package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Health is the server's /health payload (the version/compat handle).
type Health struct {
	Status string `json:"status"`
	Sha    string `json:"sha"`
}

// FetchHealth performs an unauthenticated GET <baseURL>/health. It is used by
// `vibexp version` to report the server release sha, and degrades gracefully
// (the caller ignores the error) when the server is unreachable.
func FetchHealth(ctx context.Context, hc *http.Client, baseURL string) (*Health, error) {
	url := strings.TrimRight(baseURL, "/") + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health check: status %d", resp.StatusCode)
	}
	var h Health
	if err := json.Unmarshal(body, &h); err != nil {
		return nil, fmt.Errorf("decode health: %w", err)
	}
	return &h, nil
}
