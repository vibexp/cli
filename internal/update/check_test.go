package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSuppressed(t *testing.T) {
	valid := "v1.0.0"
	cases := []struct {
		name    string
		env     map[string]string
		current string
		want    bool
	}{
		{"clean release build", nil, valid, false},
		{"no-update-check env", map[string]string{"VIBEXP_NO_UPDATE_CHECK": "1"}, valid, true},
		{"CI env", map[string]string{"CI": "true"}, valid, true},
		{"dev build", nil, "dev", true},
		{"empty version", nil, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(k string) string { return tc.env[k] }
			if got := Suppressed(getenv, tc.current); got != tc.want {
				t.Errorf("Suppressed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestShouldCheck(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	if !ShouldCheck(State{}, now) {
		t.Error("never-checked state should check")
	}
	if !ShouldCheck(State{LastCheck: now.Add(-25 * time.Hour)}, now) {
		t.Error("25h-old check should re-check")
	}
	if ShouldCheck(State{LastCheck: now.Add(-1 * time.Hour)}, now) {
		t.Error("1h-old check should not re-check")
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"1.0.0", "1.1.0", true}, // normalizes missing "v"
		{"v1.2.0", "v1.2.0", false},
		{"v2.0.0", "v1.9.9", false},
		{"dev", "v1.0.0", false}, // invalid current
		{"v1.0.0", "garbage", false},
	}
	for _, tc := range cases {
		if got := IsNewer(tc.current, tc.latest); got != tc.want {
			t.Errorf("IsNewer(%q,%q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestFetchLatestAndETag(t *testing.T) {
	const etag = `"abc123"`
	var sawIfNoneMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawIfNoneMatch = r.Header.Get("If-None-Match")
		if sawIfNoneMatch == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_, _ = w.Write([]byte(`{"tag_name":"v1.5.0","assets":[]}`))
	}))
	defer srv.Close()

	tag, gotETag, notModified, err := fetchLatest(context.Background(), srv.Client(), srv.URL, "")
	if err != nil || notModified {
		t.Fatalf("first fetch: tag=%q err=%v notModified=%v", tag, err, notModified)
	}
	if tag != "v1.5.0" || gotETag != etag {
		t.Fatalf("first fetch wrong: tag=%q etag=%q", tag, gotETag)
	}

	// Second fetch with the ETag must get a 304.
	_, _, notModified, err = fetchLatest(context.Background(), srv.Client(), srv.URL, etag)
	if err != nil || !notModified {
		t.Fatalf("second fetch should be 304: notModified=%v err=%v", notModified, err)
	}
	if sawIfNoneMatch != etag {
		t.Errorf("If-None-Match not sent: %q", sawIfNoneMatch)
	}
}
