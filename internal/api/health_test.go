package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","sha":"abc1234"}`))
	}))
	defer srv.Close()

	h, err := FetchHealth(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("FetchHealth: %v", err)
	}
	if h.Sha != "abc1234" || h.Status != "ok" {
		t.Errorf("health = %+v", h)
	}
}

func TestFetchHealthErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := FetchHealth(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Error("expected error on 500")
	}
}
