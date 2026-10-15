package searchcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

// Fabricated summary envelope: sources deliberately out of citation order, a
// memory with no slug, and a truncated document.
const summaryBody = `{"summary":"Retries back off exponentially [1].\n\nSee [2] and [3].","sources":[` +
	`{"index":2,"id":"0b1c2d3e-4f50-4617-8293-a4b5c6d7e8f9","type":"memory","title":"Retry notes","slug":"","truncated":false},` +
	`{"index":1,"id":"1c2d3e4f-5061-4728-93a4-b5c6d7e8f9a0","type":"blueprint","title":"Backoff policy","slug":"backoff-policy","truncated":false},` +
	`{"index":3,"id":"2d3e4f50-6172-4839-a4b5-c6d7e8f9a0b1","type":"artifact","title":"Incident review","slug":"incident-review","truncated":true}` +
	`],"model":"fake-model","provider_id":"3e4f5061-7283-4940-b5c6-d7e8f9a0b1c2","generated_at":"2026-01-02T03:04:05Z"}`

type summarySeen struct {
	calls int
	body  map[string]any
}

// summaryServer answers /search/summary with status + body and records the
// request; every other path is a 404.
func summaryServer(t *testing.T, seen *summarySeen, status int, resp string, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/"+teamID+"/search/summary" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		seen.calls++
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &seen.body)
		time.Sleep(delay)
		if status != http.StatusOK {
			w.Header().Set("Content-Type", "application/problem+json")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
}

// runSummaryCmd runs `search <args>` under a stand-in root that declares the
// global --timeout flag, returning stdout, stderr (with the returned error
// appended, as the real root prints it) and the exit code.
func runSummaryCmd(t *testing.T, srv *httptest.Server, rt *config.Runtime, args ...string) (string, string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.json")
	getenv := func(k string) string {
		if k == cred.EnvAPIKey {
			return "vxk_fabricated"
		}
		return ""
	}
	root := &cobra.Command{Use: "vibexp", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().Duration("timeout", 30*time.Second, "per-request timeout")
	root.AddCommand(New(func() (*cred.Store, error) { return &cred.Store{Path: path}, nil }, getenv))
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"search"}, args...))
	rt.BaseURL = srv.URL
	if rt.Team == "" {
		rt.Team = teamID
	}
	err := root.ExecuteContext(clictx.WithRuntime(context.Background(), rt))
	if err != nil {
		errBuf.WriteString(err.Error())
	}
	return out.String(), errBuf.String(), exitcode.FromError(err)
}

func TestSummaryRequestBody(t *testing.T) {
	var seen summarySeen
	srv := summaryServer(t, &seen, http.StatusOK, summaryBody, 0)
	defer srv.Close()

	_, stderr, code := runSummaryCmd(t, srv, &config.Runtime{Project: projectID, Format: "json"},
		"how do we configure retries", "--summary", "--type", "memories")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, stderr)
	}
	want := map[string]any{"query": "how do we configure retries", "types": []any{"memories"}, "project_id": projectID}
	got, _ := json.Marshal(seen.body)
	wantJSON, _ := json.Marshal(want)
	if string(got) != string(wantJSON) {
		t.Errorf("body = %s, want %s", got, wantJSON)
	}

	// Without --type or --project the body carries only the query.
	seen = summarySeen{}
	if _, stderr, code := runSummaryCmd(t, srv, &config.Runtime{Format: "json"}, "q", "--summary"); code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, stderr)
	}
	if got, _ := json.Marshal(seen.body); string(got) != `{"query":"q"}` {
		t.Errorf("body = %s, want only the query", got)
	}
}

func TestSummaryTextOutput(t *testing.T) {
	var seen summarySeen
	srv := summaryServer(t, &seen, http.StatusOK, summaryBody, 0)
	defer srv.Close()

	for _, format := range []string{"", "text", "table"} {
		out, stderr, code := runSummaryCmd(t, srv, &config.Runtime{Format: format}, "q", "--summary")
		if code != 0 {
			t.Fatalf("format %q: exit = %d, stderr=%q", format, code, stderr)
		}
		want := "Retries back off exponentially [1].\n\nSee [2] and [3].\n" +
			"\n" +
			"[1] blueprint  Backoff policy  backoff-policy\n" +
			"[2] memory  Retry notes  0b1c2d3e-4f50-4617-8293-a4b5c6d7e8f9\n" +
			"[3] artifact  Incident review  incident-review (truncated)\n"
		if out != want {
			t.Errorf("format %q: stdout =\n%q\nwant\n%q", format, out, want)
		}
	}
}

func TestSummaryTextWithoutSources(t *testing.T) {
	var seen summarySeen
	srv := summaryServer(t, &seen, http.StatusOK, `{"summary":"Just this.\n","sources":[]}`, 0)
	defer srv.Close()

	out, _, code := runSummaryCmd(t, srv, &config.Runtime{}, "q", "--summary")
	if code != 0 || out != "Just this.\n" {
		t.Errorf("exit = %d, stdout = %q; want only the answer", code, out)
	}
}

func TestSummaryEnvelopeFormats(t *testing.T) {
	var seen summarySeen
	srv := summaryServer(t, &seen, http.StatusOK, summaryBody, 0)
	defer srv.Close()

	out, _, code := runSummaryCmd(t, srv, &config.Runtime{Format: "json"}, "q", "--summary")
	if code != 0 {
		t.Fatalf("json exit = %d", code)
	}
	var got, want any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json output is not JSON: %v\n%s", err, out)
	}
	_ = json.Unmarshal([]byte(summaryBody), &want)
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("json should be the response envelope:\n%s\nwant\n%s", gotJSON, wantJSON)
	}

	out, _, code = runSummaryCmd(t, srv, &config.Runtime{JQ: ".model"}, "q", "--summary")
	if code != 0 || strings.TrimSpace(out) != `"fake-model"` {
		t.Errorf("--jq should run over the envelope: exit = %d, out = %q", code, out)
	}

	out, _, code = runSummaryCmd(t, srv, &config.Runtime{Format: "yaml"}, "q", "--summary")
	if code != 0 || !strings.Contains(out, "model: fake-model") {
		t.Errorf("yaml should render the envelope: exit = %d, out = %q", code, out)
	}
}

func TestSummaryRejectsPaging(t *testing.T) {
	var seen summarySeen
	srv := summaryServer(t, &seen, http.StatusOK, summaryBody, 0)
	defer srv.Close()

	for _, args := range [][]string{{"--limit", "5"}, {"--page", "2"}} {
		_, stderr, code := runSummaryCmd(t, srv, &config.Runtime{}, append([]string{"q", "--summary"}, args...)...)
		if code != exitcode.UsageErr {
			t.Errorf("%v: exit = %d, want 2", args, code)
		}
		if !strings.Contains(stderr, args[0]) {
			t.Errorf("%v: the message should name the flag: %q", args, stderr)
		}
	}
	if seen.calls != 0 {
		t.Errorf("a usage error must send no request, got %d", seen.calls)
	}
}

func TestSummaryErrors(t *testing.T) {
	cases := []struct {
		status int
		code   string
		hint   string
		exit   int
	}{
		{http.StatusConflict, "AI_SUMMARY_DISABLED", "AI summaries are disabled for this team", 1},
		{http.StatusConflict, "AI_SUMMARY_NO_PROVIDER", "no model provider is configured", 1},
		{http.StatusUnprocessableEntity, "AI_SUMMARY_NO_RESULTS", "found nothing to summarize", 1},
		{http.StatusBadGateway, "AI_SUMMARY_PROVIDER_UNREACHABLE", "could not be reached", 1},
		{http.StatusBadGateway, "AI_SUMMARY_UNAUTHORIZED", "rejected its credentials", 1},
		{http.StatusBadGateway, "AI_SUMMARY_MODEL_ERROR", "failed to produce a summary", 1},
		{http.StatusGatewayTimeout, "AI_SUMMARY_TIMEOUT", "timed out", 1},
		{http.StatusBadRequest, "VALIDATION_ERROR", "query is too long", 2},
		{http.StatusConflict, "SOMETHING_NEW", "query is too long", 1},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			var seen summarySeen
			resp := `{"title":"x","status":` + strconv.Itoa(c.status) + `,"code":"` + c.code +
				`","detail":"query is too long","request_id":"req-fabricated-1"}`
			srv := summaryServer(t, &seen, c.status, resp, 0)
			defer srv.Close()

			out, stderr, code := runSummaryCmd(t, srv, &config.Runtime{}, "q", "--summary")
			if code != c.exit {
				t.Errorf("exit = %d, want %d", code, c.exit)
			}
			if out != "" {
				t.Errorf("stdout must stay empty on error: %q", out)
			}
			first, _, _ := strings.Cut(stderr, "\n")
			if !strings.Contains(first, c.hint) {
				t.Errorf("first stderr line %q should contain %q", first, c.hint)
			}
			if !strings.Contains(stderr, "req-fabricated-1") {
				t.Errorf("stderr should keep the request_id: %q", stderr)
			}
		})
	}
}

func TestSummaryTimeout(t *testing.T) {
	var seen summarySeen
	srv := summaryServer(t, &seen, http.StatusOK, summaryBody, 300*time.Millisecond)
	defer srv.Close()

	// A short runtime timeout is raised for the summary when --timeout is unset…
	if _, stderr, code := runSummaryCmd(t, srv, &config.Runtime{Timeout: 50 * time.Millisecond}, "q", "--summary"); code != 0 {
		t.Errorf("an unset --timeout should be raised for the summary: exit = %d, stderr=%q", code, stderr)
	}
	// …but an explicit --timeout is honored.
	if _, _, code := runSummaryCmd(t, srv, &config.Runtime{Timeout: 50 * time.Millisecond}, "--timeout", "50ms", "q", "--summary"); code == 0 {
		t.Error("an explicit --timeout should be honored")
	}
}
