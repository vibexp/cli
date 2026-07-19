package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/vibexp/cli/internal/version"
)

// checkInterval bounds the GitHub Releases query to at most once per day.
const checkInterval = 24 * time.Hour

// checkTimeout caps the network check so it can never noticeably delay the CLI.
const checkTimeout = 2 * time.Second

// DefaultAPIBase is the GitHub API host the release check/update query; tests
// override it with a local stub.
const DefaultAPIBase = "https://api.github.com"

const defaultAPIBase = DefaultAPIBase

const releaseRepo = "vibexp/cli"

// release is the subset of the GitHub Releases API we consume.
type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// Suppressed reports whether the background check must not run: opt-out env vars,
// CI, or a non-release build (dev/source) where there is nothing to compare to.
func Suppressed(getenv func(string) string, current string) bool {
	if getenv("VIBEXP_NO_UPDATE_CHECK") != "" {
		return true
	}
	if getenv("CI") != "" {
		return true
	}
	// A dev/unversioned build has no meaningful "latest" to compare against.
	return !semver.IsValid(normalize(current))
}

// ShouldCheck reports whether enough time has elapsed to query GitHub again.
func ShouldCheck(st State, now time.Time) bool {
	return now.Sub(st.LastCheck) >= checkInterval
}

// fetchLatest queries the latest release, sending If-None-Match for a cheap 304.
// It returns the tag and ETag; notModified is true when the cached ETag still
// matches (tag is then empty).
func fetchLatest(ctx context.Context, client *http.Client, apiBase, etag string) (tag, newETag string, notModified bool, err error) {
	url := apiBase + "/repos/" + releaseRepo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "vibexp-cli/"+version.Version)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		return "", etag, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", false, fmt.Errorf("github releases: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", false, err
	}
	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", "", false, err
	}
	return rel.TagName, resp.Header.Get("ETag"), false, nil
}

// IsNewer reports whether latest is a strictly higher semver than current.
func IsNewer(current, latest string) bool {
	c, l := normalize(current), normalize(latest)
	if !semver.IsValid(c) || !semver.IsValid(l) {
		return false
	}
	return semver.Compare(l, c) > 0
}

// normalize coerces a version/tag into a semver-comparable form (ensures a
// leading "v"; trims spaces).
func normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}
