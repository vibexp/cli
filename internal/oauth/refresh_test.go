package oauth

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vibexp/cli/internal/cred"
)

// oauthRefresher builds a Refresher over a temp cred store seeded with an
// expired OAuth entry.
func seedRefresher(t *testing.T, st *asState, tokenEndpoint string) (*cred.Store, *Refresher) {
	t.Helper()
	dir := t.TempDir()
	store := &cred.Store{Path: filepath.Join(dir, "credentials.json")}
	// Seed an already-expired token whose refresh token the mock accepts.
	access, refresh := st.issue()
	if err := store.Save("dev", cred.Entry{
		Type:         cred.TypeOAuth,
		ClientID:     "client-1",
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    time.Now().Add(-time.Hour), // expired
	}); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	r := &Refresher{
		Store:         store,
		ContextName:   "dev",
		TokenEndpoint: tokenEndpoint,
		ClientID:      "client-1",
		LockPath:      filepath.Join(dir, "credentials.lock"),
	}
	return store, r
}

func TestRefresherRefreshesExpiredAndPersistsRotation(t *testing.T) {
	st := &asState{}
	srv := mockAS(t, st)
	defer srv.Close()

	store, r := seedRefresher(t, st, srv.URL+"/token")
	r.HTTPClient = srv.Client()

	before, _ := store.Get("dev")

	tok, err := r.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok == before.AccessToken {
		t.Error("access token not refreshed")
	}

	// Rotation persisted atomically.
	after, _ := store.Get("dev")
	if after.RefreshToken == before.RefreshToken {
		t.Error("rotated refresh token not persisted")
	}
	if after.AccessToken != tok {
		t.Error("persisted access token != returned token")
	}
	if !after.ExpiresAt.After(time.Now()) {
		t.Error("persisted expiry not refreshed into the future")
	}
}

func TestRefresherValidTokenNoRefresh(t *testing.T) {
	st := &asState{}
	srv := mockAS(t, st)
	defer srv.Close()

	dir := t.TempDir()
	store := &cred.Store{Path: filepath.Join(dir, "credentials.json")}
	_ = store.Save("dev", cred.Entry{
		Type: cred.TypeOAuth, ClientID: "c", AccessToken: "still-good",
		RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour),
	})
	r := &Refresher{HTTPClient: srv.Client(), Store: store, ContextName: "dev",
		TokenEndpoint: srv.URL + "/token", ClientID: "c", LockPath: filepath.Join(dir, "lock")}

	tok, err := r.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok != "still-good" {
		t.Errorf("token = %q, want still-good (no refresh)", tok)
	}
	if st.refreshCount != 0 {
		t.Errorf("refreshCount = %d, want 0", st.refreshCount)
	}
}

func TestRefresherReuseDetectionWipesSession(t *testing.T) {
	st := &asState{}
	srv := mockAS(t, st)
	defer srv.Close()

	dir := t.TempDir()
	store := &cred.Store{Path: filepath.Join(dir, "credentials.json")}
	// Refresh token the mock will reject (not in validRefresh) -> invalid_grant.
	_ = store.Save("dev", cred.Entry{
		Type: cred.TypeOAuth, ClientID: "c", AccessToken: "expired",
		RefreshToken: "already-used", ExpiresAt: time.Now().Add(-time.Hour),
	})
	r := &Refresher{HTTPClient: srv.Client(), Store: store, ContextName: "dev",
		TokenEndpoint: srv.URL + "/token", ClientID: "c", LockPath: filepath.Join(dir, "lock")}

	_, err := r.AccessToken(context.Background())
	if err != ErrReauthRequired {
		t.Fatalf("err = %v, want ErrReauthRequired", err)
	}
	// Session must be wiped.
	if e, _ := store.Get("dev"); e != nil {
		t.Errorf("session not wiped after reuse detection: %+v", e)
	}
}

func TestRefresherConcurrentSingleRefresh(t *testing.T) {
	st := &asState{}
	srv := mockAS(t, st)
	defer srv.Close()

	store, _ := seedRefresher(t, st, srv.URL+"/token")
	lockPath := filepath.Join(filepath.Dir(store.Path), "credentials.lock")

	newR := func() *Refresher {
		return &Refresher{HTTPClient: srv.Client(), Store: store, ContextName: "dev",
			TokenEndpoint: srv.URL + "/token", ClientID: "client-1", LockPath: lockPath}
	}

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	toks := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			toks[i], errs[i] = newR().AccessToken(context.Background())
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	// The file lock + re-read must collapse this to exactly one refresh.
	st.mu.Lock()
	got := st.refreshCount
	st.mu.Unlock()
	if got != 1 {
		t.Errorf("refreshCount = %d, want exactly 1", got)
	}
	// All callers get the same rotated access token.
	for i := 1; i < n; i++ {
		if toks[i] != toks[0] {
			t.Errorf("token mismatch across concurrent refreshers: %q vs %q", toks[i], toks[0])
		}
	}
}
