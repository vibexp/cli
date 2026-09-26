package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

// metaWriteNoun describes where one noun's create/update endpoints live and the
// extra args its create needs. All data is fabricated.
type metaWriteNoun struct {
	noun       string
	id         string   // the <id>/<slug> positional
	collection string   // POST path
	item       string   // GET/PUT path
	createArgs []string // required create args besides the body
}

var metaWriteNouns = []metaWriteNoun{
	{"memory", "m-1", "/api/v1/0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01/memories", "/api/v1/0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01/memories/m-1", nil},
	{"artifact", "art-1", "/api/v1/0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01/artifacts", "/api/v1/0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01/artifacts/3a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c61/art-1", []string{"art-1", "--title", "T"}},
	{"blueprint", "bp-1", "/api/v1/0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01/blueprints", "/api/v1/0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01/blueprints/3a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c61/bp-1", []string{"bp-1", "--title", "T"}},
}

// metaWriteCapture records every request a metadata write made.
type metaWriteCapture struct {
	methods []string
	body    map[string]any // last POST/PUT body
}

// metaWriteServer serves n's endpoints; the stored item carries metadata
// {"a":"1","b":"2"}.
func metaWriteServer(t *testing.T, n metaWriteNoun, cap *metaWriteCapture) *httptest.Server {
	t.Helper()
	record := func(w http.ResponseWriter, r *http.Request) {
		cap.methods = append(cap.methods, r.Method)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			cap.body = nil
			_ = json.NewDecoder(r.Body).Decode(&cap.body)
		}
		_, _ = w.Write([]byte(`{"id":"` + n.id + `","project_id":"3a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c61","status":"active","metadata":{"a":"1","b":"2"}}`))
	}
	mux := http.NewServeMux()
	mux.HandleFunc(n.collection, record)
	mux.HandleFunc(n.item, record)
	return httptest.NewServer(mux)
}

func metaJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMetadataCreateSendsMetadata(t *testing.T) {
	for _, n := range metaWriteNouns {
		t.Run(n.noun, func(t *testing.T) {
			var cap metaWriteCapture
			srv := metaWriteServer(t, n, &cap)
			defer srv.Close()
			cfg, cs := apiFixture(t, srv.URL, "0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01")

			args := append([]string{n.noun, "create"}, n.createArgs...)
			args = append(args, "--project", "3a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c61", "--body-file", "-",
				"--metadata", "source=cli", "--metadata", "area=auth",
				"--metadata-json", `{"priority":2,"reviewed":true}`)
			if _, errOut, code := runAuth(t, cfg, cs, nil, "body", args...); code != 0 {
				t.Fatalf("create exit = %d, stderr=%q", code, errOut)
			}
			want := `{"area":"auth","priority":2,"reviewed":true,"source":"cli"}`
			if got := metaJSON(t, cap.body["metadata"]); got != want {
				t.Errorf("create metadata = %s, want %s", got, want)
			}
		})
	}
}

func TestMetadataCreateOmittedWithoutFlags(t *testing.T) {
	n := metaWriteNouns[0]
	var cap metaWriteCapture
	srv := metaWriteServer(t, n, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01")
	if _, _, code := runAuth(t, cfg, cs, nil, "body", "memory", "create", "--project", "3a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c61", "--body-file", "-"); code != 0 {
		t.Fatalf("create exit = %d", code)
	}
	if _, ok := cap.body["metadata"]; ok {
		t.Errorf("metadata sent without a flag: %+v", cap.body)
	}
}

func TestMetadataUpdateMergesUnsetsReplaces(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		want        string
		wantMethods string
	}{
		{"merge", []string{"--metadata", "b=3"}, `{"a":"1","b":"3"}`, `["GET","PUT"]`},
		{"unset", []string{"--unset-metadata", "a"}, `{"b":"2"}`, `["GET","PUT"]`},
		{"replace", []string{"--replace-metadata", "--metadata", "c=1"}, `{"c":"1"}`, `["PUT"]`},
	}
	for _, n := range metaWriteNouns {
		for _, tc := range cases {
			t.Run(n.noun+"/"+tc.name, func(t *testing.T) {
				var cap metaWriteCapture
				srv := metaWriteServer(t, n, &cap)
				defer srv.Close()
				cfg, cs := apiFixture(t, srv.URL, "0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01")

				args := append([]string{"--project", "3a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c61", n.noun, "update", n.id}, tc.args...)
				if _, errOut, code := runAuth(t, cfg, cs, nil, "", args...); code != 0 {
					t.Fatalf("update exit = %d, stderr=%q", code, errOut)
				}
				if got := metaJSON(t, cap.body["metadata"]); got != tc.want {
					t.Errorf("update metadata = %s, want %s", got, tc.want)
				}
				if got := metaJSON(t, cap.methods); got != tc.wantMethods {
					t.Errorf("requests = %s, want %s", got, tc.wantMethods)
				}
			})
		}
	}
}

func TestMetadataMalformedIsUsageAndSendsNothing(t *testing.T) {
	bad := [][]string{
		{"--metadata", "novalue"},
		{"--metadata", "=x"},
		{"--metadata-json", "{nope"},
		{"--metadata-json", "[1]"},
		{"--metadata-json", ""},
	}
	for _, n := range metaWriteNouns {
		for _, flags := range bad {
			var cap metaWriteCapture
			srv := metaWriteServer(t, n, &cap)
			cfg, cs := apiFixture(t, srv.URL, "0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01")

			create := append(append([]string{n.noun, "create"}, n.createArgs...), "--project", "3a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c61", "--body-file", "-")
			if _, _, code := runAuth(t, cfg, cs, nil, "body", append(create, flags...)...); code != exitcode.UsageErr {
				t.Errorf("%s create %v exit = %d, want 2", n.noun, flags, code)
			}
			update := []string{"--project", "3a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c61", n.noun, "update", n.id}
			if _, _, code := runAuth(t, cfg, cs, nil, "", append(update, flags...)...); code != exitcode.UsageErr {
				t.Errorf("%s update %v exit = %d, want 2", n.noun, flags, code)
			}
			if len(cap.methods) != 0 {
				t.Errorf("%s %v sent requests: %v", n.noun, flags, cap.methods)
			}
			srv.Close()
		}
	}
}

func TestMetadataBodyAndJSONBothStdinRejected(t *testing.T) {
	n := metaWriteNouns[0]
	var cap metaWriteCapture
	srv := metaWriteServer(t, n, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01")
	if _, _, code := runAuth(t, cfg, cs, nil, "body", "memory", "create", "--project", "3a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c61",
		"--body-file", "-", "--metadata-json", "-"); code != exitcode.UsageErr {
		t.Errorf("both stdin exit = %d, want 2", code)
	}
	if len(cap.methods) != 0 {
		t.Errorf("sent requests: %v", cap.methods)
	}
}
