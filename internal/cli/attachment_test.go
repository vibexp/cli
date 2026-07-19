package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

type attachmentCapture struct {
	ownerID     string
	ownerType   string
	fileName    string
	fileCT      string
	fileContent string
	listQuery   url.Values
	deleted     string
}

func attachmentServer(t *testing.T, cap *attachmentCapture) *httptest.Server {
	t.Helper()
	const base = "/api/v1/the-team/attachments"
	mux := http.NewServeMux()
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			cap.listQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"attachments":[{"id":"att-1","file_name":"report.txt","size_bytes":21,"content_type":"text/plain","created_at":"2026-02-01T00:00:00Z"}],"page":1,"per_page":50,"total_count":1,"total_pages":1}`))
		case http.MethodPost:
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			cap.ownerID = r.FormValue("owner_id")
			cap.ownerType = r.FormValue("owner_type")
			file, hdr, err := r.FormFile("file")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer func() { _ = file.Close() }()
			cap.fileName = hdr.Filename
			cap.fileCT = hdr.Header.Get("Content-Type")
			data, _ := io.ReadAll(file)
			cap.fileContent = string(data)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"att-new","file_name":"` + hdr.Filename + `","size_bytes":21,"content_type":"text/plain","created_at":"2026-02-02T00:00:00Z"}`))
		}
	})
	mux.HandleFunc(base+"/att-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			cap.deleted = "att-1"
			w.WriteHeader(http.StatusNoContent)
		}
	})
	return httptest.NewServer(mux)
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := t.TempDir() + "/" + name
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAttachmentUploadMultipart(t *testing.T) {
	var cap attachmentCapture
	srv := attachmentServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	f := writeTemp(t, "report.txt", "hello attachment body")
	out, _, code := runAuth(t, cfg, cs, nil, "", "attachment", "upload", f, "--owner-id", "own-1")
	if code != 0 {
		t.Fatalf("upload exit = %d, out=%q", code, out)
	}
	if cap.ownerID != "own-1" || cap.ownerType != "artifact" {
		t.Errorf("owner fields wrong: id=%q type=%q", cap.ownerID, cap.ownerType)
	}
	if cap.fileName != "report.txt" || cap.fileContent != "hello attachment body" {
		t.Errorf("file wrong: name=%q content=%q", cap.fileName, cap.fileContent)
	}
	if !strings.HasPrefix(cap.fileCT, "text/plain") {
		t.Errorf("content type not detected from extension: %q", cap.fileCT)
	}
	if !strings.Contains(out, "att-new") {
		t.Errorf("upload output missing created attachment: %q", out)
	}
}

func TestAttachmentUploadContentTypeOverride(t *testing.T) {
	var cap attachmentCapture
	srv := attachmentServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// A file with no useful extension; override the content type explicitly.
	f := writeTemp(t, "blob", "some bytes")
	_, _, code := runAuth(t, cfg, cs, nil, "", "attachment", "upload", f,
		"--owner-id", "own-1", "--owner-type", "artifact", "--content-type", "application/json")
	if code != 0 {
		t.Fatalf("upload exit = %d", code)
	}
	if cap.fileCT != "application/json" {
		t.Errorf("--content-type override not applied: %q", cap.fileCT)
	}
}

func TestAttachmentUploadRequiresOwner(t *testing.T) {
	var cap attachmentCapture
	srv := attachmentServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	f := writeTemp(t, "report.txt", "x")
	if _, _, code := runAuth(t, cfg, cs, nil, "", "attachment", "upload", f); code != exitcode.UsageErr {
		t.Errorf("upload without --owner-id exit = %d, want 2", code)
	}
	// Missing file → usage error.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "attachment", "upload", "/no/such/file", "--owner-id", "own-1"); code != exitcode.UsageErr {
		t.Errorf("upload missing file exit = %d, want 2", code)
	}
}

func TestAttachmentList(t *testing.T) {
	var cap attachmentCapture
	srv := attachmentServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Missing --owner-id → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "attachment", "list"); code != exitcode.UsageErr {
		t.Errorf("list without --owner-id exit = %d, want 2", code)
	}
	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "attachment", "list", "--owner-id", "own-9", "--limit", "3")
	if code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	if cap.listQuery.Get("owner_id") != "own-9" || cap.listQuery.Get("owner_type") != "artifact" || cap.listQuery.Get("limit") != "3" {
		t.Errorf("list query wrong: %v", cap.listQuery)
	}
	if !strings.Contains(out, `"id":"att-1"`) {
		t.Errorf("list json missing attachment: %q", out)
	}
}

func TestAttachmentDelete(t *testing.T) {
	var cap attachmentCapture
	srv := attachmentServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	if _, _, code := runAuth(t, cfg, cs, nil, "", "attachment", "delete", "att-1"); code != exitcode.UsageErr {
		t.Errorf("delete without --yes exit = %d, want 2", code)
	}
	if cap.deleted != "" {
		t.Error("must not delete without confirmation")
	}
	_, errOut, code := runAuth(t, cfg, cs, nil, "", "attachment", "delete", "att-1", "--yes")
	if code != 0 {
		t.Fatalf("delete --yes exit = %d", code)
	}
	if cap.deleted != "att-1" {
		t.Error("delete --yes did not delete")
	}
	if !strings.Contains(errOut, "Deleted attachment att-1") {
		t.Errorf("delete should confirm: %q", errOut)
	}
}
