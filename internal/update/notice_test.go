package update

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubReleases returns a server serving the given tag and counts hits.
func stubReleases(t *testing.T, tag string, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Header().Set("ETag", `"e"`)
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `","assets":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func run(t *testing.T, p noticeParams) (string, State) {
	t.Helper()
	var buf bytes.Buffer
	p.w = &buf
	notify(context.Background(), p)
	return buf.String(), LoadState(p.statePath)
}

func baseParams(t *testing.T, srvURL, statePath, current string) noticeParams {
	return noticeParams{
		getenv:    func(string) string { return "" },
		statePath: statePath,
		apiBase:   srvURL,
		client:    &http.Client{Timeout: 2 * time.Second},
		current:   current,
		now:       time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC),
	}
}

func TestNotifyOutdatedPrintsOnce(t *testing.T) {
	var hits int32
	srv := stubReleases(t, "v2.0.0", &hits)
	statePath := filepath.Join(t.TempDir(), "state.json")

	out, st := run(t, baseParams(t, srv.URL, statePath, "v1.0.0"))
	if !strings.Contains(out, "v1.0.0 → v2.0.0") {
		t.Errorf("expected upgrade notice, got %q", out)
	}
	if !strings.Contains(out, "run:") {
		t.Errorf("notice should carry an upgrade hint: %q", out)
	}
	if st.LatestSeen != "v2.0.0" || st.LastCheck.IsZero() {
		t.Errorf("state not persisted: %+v", st)
	}
}

func TestNotifyUpToDateSilent(t *testing.T) {
	var hits int32
	srv := stubReleases(t, "v1.0.0", &hits)
	statePath := filepath.Join(t.TempDir(), "state.json")
	out, _ := run(t, baseParams(t, srv.URL, statePath, "v1.0.0"))
	if out != "" {
		t.Errorf("up-to-date should be silent, got %q", out)
	}
}

// TestNotifyCacheAvoidsNetwork: a recent check serves the notice from cached
// state without hitting GitHub again.
func TestNotifyCacheAvoidsNetwork(t *testing.T) {
	var hits int32
	srv := stubReleases(t, "v2.0.0", &hits)
	statePath := filepath.Join(t.TempDir(), "state.json")

	p := baseParams(t, srv.URL, statePath, "v1.0.0")
	// Seed a fresh state (checked 1h ago) that already saw a newer release.
	_ = SaveState(statePath, State{LastCheck: p.now.Add(-1 * time.Hour), LatestSeen: "v2.0.0"})

	out, _ := run(t, p)
	if !strings.Contains(out, "v2.0.0") {
		t.Errorf("cached notice expected: %q", out)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Errorf("must not hit GitHub within the interval, hits=%d", hits)
	}
}

func TestNotifySuppressed(t *testing.T) {
	var hits int32
	srv := stubReleases(t, "v2.0.0", &hits)
	statePath := filepath.Join(t.TempDir(), "state.json")

	p := baseParams(t, srv.URL, statePath, "v1.0.0")
	p.getenv = func(k string) string {
		if k == "VIBEXP_NO_UPDATE_CHECK" {
			return "1"
		}
		return ""
	}
	out, _ := run(t, p)
	if out != "" || atomic.LoadInt32(&hits) != 0 {
		t.Errorf("suppressed check must be silent and network-free: out=%q hits=%d", out, hits)
	}
}
