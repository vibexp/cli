package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testDoer builds a Doer that never really sleeps and records the backoff
// durations it would have waited.
func testDoer() (*Doer, *[]time.Duration) {
	d := NewDoer(0)
	var waits []time.Duration
	d.sleep = func(w time.Duration) { waits = append(waits, w) }
	d.jitter = func(time.Duration) time.Duration { return 0 }
	return d, &waits
}

func TestDoerRetriesGetOn503(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	d, waits := testDoer()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := d.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("attempts = %d, want 3 (max)", got)
	}
	if len(*waits) != 2 {
		t.Errorf("backoff sleeps = %d, want 2 between 3 attempts", len(*waits))
	}
}

func TestDoerRetriesThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d, _ := testDoer()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := d.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 after retries", resp.StatusCode)
	}
}

func TestDoerDoesNotRetryPost(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		d, _ := testDoer()
		req, _ := http.NewRequest(method, srv.URL, strings.NewReader("{}"))
		resp, err := d.Do(req)
		if err != nil {
			t.Fatalf("%s Do: %v", method, err)
		}
		_ = resp.Body.Close()
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Errorf("%s attempts = %d, want 1 (no retry)", method, got)
		}
		srv.Close()
	}
}

func TestDoerHonorsRetryAfter(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d, waits := testDoer()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, _ := d.Do(req)
	_ = resp.Body.Close()
	if len(*waits) != 1 || (*waits)[0] != 2*time.Second {
		t.Errorf("waits = %v, want [2s] from Retry-After", *waits)
	}
}

func TestDoerSetsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d, _ := testDoer()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, _ := d.Do(req)
	_ = resp.Body.Close()
	if !strings.HasPrefix(gotUA, "vibexp-cli/") {
		t.Errorf("User-Agent = %q, want vibexp-cli/ prefix", gotUA)
	}
}
