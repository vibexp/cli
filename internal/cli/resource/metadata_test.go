package resource_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/exitcode"
)

// metaFlags binds the metadata flags to a throwaway command and parses args.
func metaFlags(t *testing.T, update bool, args ...string) *resource.MetadataFlags {
	t.Helper()
	var f resource.MetadataFlags
	cmd := &cobra.Command{Use: "x"}
	resource.AddMetadataFlags(cmd, &f, update)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return &f
}

// asJSON renders a metadata object canonically (sorted keys) for comparison.
func asJSON(t *testing.T, m map[string]any) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func isUsage(err error) bool {
	var ce *exitcode.CodedError
	return errors.As(err, &ce) && ce.Code == exitcode.UsageErr
}

func TestMetadataFlagsBuild(t *testing.T) {
	file := filepath.Join(t.TempDir(), "meta.json")
	if err := os.WriteFile(file, []byte(`{"from":"file","n":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"pairs", []string{"--metadata", "source=cli", "--metadata", "area=auth"}, "", `{"area":"auth","source":"cli"}`},
		{"empty value", []string{"--metadata", "k="}, "", `{"k":""}`},
		{"value with equals", []string{"--metadata", "q=a=b"}, "", `{"q":"a=b"}`},
		{"typed json", []string{"--metadata-json", `{"priority":2,"reviewed":true,"ratio":0.5}`}, "", `{"priority":2,"ratio":0.5,"reviewed":true}`},
		{"pair wins over json", []string{"--metadata-json", `{"a":1,"b":2}`, "--metadata", "b=3"}, "", `{"a":1,"b":"3"}`},
		{"json from file", []string{"--metadata-json", "@" + file}, "", `{"from":"file","n":1}`},
		{"json from stdin", []string{"--metadata-json", "-"}, `{"nested":{"x":[1,2]}}`, `{"nested":{"x":[1,2]}}`},
		{"none", nil, "", `{}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := metaFlags(t, false, tc.args...).Build(strings.NewReader(tc.stdin))
			if err != nil {
				t.Fatal(err)
			}
			if s := asJSON(t, got); s != tc.want {
				t.Errorf("Build = %s, want %s", s, tc.want)
			}
		})
	}
}

func TestMetadataFlagsBuildRejects(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no equals", []string{"--metadata", "novalue"}},
		{"empty key", []string{"--metadata", "=x"}},
		{"invalid json", []string{"--metadata-json", "{nope"}},
		{"empty json", []string{"--metadata-json", ""}},
		{"json array", []string{"--metadata-json", "[1,2]"}},
		{"json scalar", []string{"--metadata-json", `"x"`}},
		{"json null", []string{"--metadata-json", "null"}},
		{"trailing data", []string{"--metadata-json", `{"a":1} {"b":2}`}},
		{"trailing brace", []string{"--metadata-json", `{"a":1}}`}},
		{"trailing bracket", []string{"--metadata-json", `{"a":1}]`}},
		{"missing file", []string{"--metadata-json", "@/nonexistent/meta.json"}},
		{"set and unset", []string{"--metadata", "a=1", "--unset-metadata", "a"}},
		{"empty unset", []string{"--unset-metadata", ""}},
		{"replace and unset", []string{"--replace-metadata", "--unset-metadata", "a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := metaFlags(t, true, tc.args...).Build(strings.NewReader(""))
			if !isUsage(err) {
				t.Errorf("Build(%v) err = %v, want a usage error", tc.args, err)
			}
		})
	}
}

func TestMetadataFlagsForUpdateMerge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"i-1","metadata":{"a":"1","b":"2"}}`))
	}))
	defer srv.Close()
	client := rawClient(t, srv)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"overlay keeps other keys", []string{"--metadata", "b=3"}, `{"a":"1","b":"3"}`},
		{"unset removes only that key", []string{"--unset-metadata", "a"}, `{"b":"2"}`},
		{"unset absent key is a no-op", []string{"--unset-metadata", "zz"}, `{"a":"1","b":"2"}`},
		{"json and pairs overlay", []string{"--metadata-json", `{"n":2}`, "--metadata", "c=x"}, `{"a":"1","b":"2","c":"x","n":2}`},
		{"replace drops the rest", []string{"--replace-metadata", "--metadata", "c=1"}, `{"c":"1"}`},
		{"replace alone clears", []string{"--replace-metadata"}, `{}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := metaFlags(t, true, tc.args...).ForUpdate(context.Background(), client, "/item", strings.NewReader(""))
			if err != nil {
				t.Fatal(err)
			}
			if s := asJSON(t, got); s != tc.want {
				t.Errorf("ForUpdate = %s, want %s", s, tc.want)
			}
		})
	}
}

func TestMetadataFlagsSetAndStdin(t *testing.T) {
	if metaFlags(t, true).Set() {
		t.Error("Set() with no flags = true")
	}
	for _, args := range [][]string{
		{"--metadata", "a=1"}, {"--metadata-json", "{}"}, {"--metadata-json", ""}, {"--unset-metadata", "a"}, {"--replace-metadata"},
	} {
		if !metaFlags(t, true, args...).Set() {
			t.Errorf("Set() with %v = false", args)
		}
	}
	if err := metaFlags(t, false, "--metadata-json", "-").CheckStdin("-"); !isUsage(err) {
		t.Errorf("CheckStdin both stdin = %v, want usage error", err)
	}
	if err := metaFlags(t, false, "--metadata-json", "-").CheckStdin("body.md"); err != nil {
		t.Errorf("CheckStdin = %v", err)
	}
	// Create commands do not expose the update-only flags.
	cmd := &cobra.Command{Use: "x"}
	resource.AddMetadataFlags(cmd, &resource.MetadataFlags{}, false)
	if cmd.Flags().Lookup("unset-metadata") != nil || cmd.Flags().Lookup("replace-metadata") != nil {
		t.Error("create binds update-only flags")
	}
}

func TestMetadataFlagsForUpdate(t *testing.T) {
	var gets int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/item":
			_, _ = w.Write([]byte(`{"id":"i-1","metadata":{"a":"1","b":"2","big":12345678901234567890}}`))
		case "/bare":
			_, _ = w.Write([]byte(`{"id":"i-2"}`))
		case "/garbled":
			_, _ = w.Write([]byte(`not json`))
		default:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"request_id":"req-fab"}`))
		}
	}))
	defer srv.Close()
	client := rawClient(t, srv)
	ctx := context.Background()

	got, err := metaFlags(t, true, "--metadata", "b=3").ForUpdate(ctx, client, "/item", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if s := asJSON(t, got); s != `{"a":"1","b":"3","big":12345678901234567890}` {
		t.Errorf("merge = %s", s)
	}
	got, err = metaFlags(t, true, "--metadata", "c=1").ForUpdate(ctx, client, "/bare", strings.NewReader(""))
	if err != nil || asJSON(t, got) != `{"c":"1"}` {
		t.Errorf("merge onto no metadata = %v, %v", got, err)
	}

	gets = 0
	got, err = metaFlags(t, true, "--replace-metadata", "--metadata", "c=1").ForUpdate(ctx, client, "/item", strings.NewReader(""))
	if err != nil || asJSON(t, got) != `{"c":"1"}` || gets != 0 {
		t.Errorf("replace = %v, %v, gets=%d (want no GET)", got, err, gets)
	}
	if _, err := metaFlags(t, true, "--metadata", "bad").ForUpdate(ctx, client, "/item", strings.NewReader("")); !isUsage(err) || gets != 0 {
		t.Errorf("malformed = %v, gets=%d (want usage error, no GET)", err, gets)
	}
	if _, err := metaFlags(t, true, "--metadata", "a=1").ForUpdate(ctx, client, "/missing", strings.NewReader("")); err == nil {
		t.Error("404 on GET: want an error")
	}
	if _, err := metaFlags(t, true, "--metadata", "a=1").ForUpdate(ctx, client, "/garbled", strings.NewReader("")); err == nil {
		t.Error("undecodable GET body: want an error")
	}
}
