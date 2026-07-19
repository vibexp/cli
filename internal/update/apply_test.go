package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/version"
)

// makeArchive builds a release archive for the host platform containing a single
// binary entry with the given content; returns the bytes and the asset filename.
func makeArchive(t *testing.T, tag, content string) ([]byte, string) {
	t.Helper()
	goos, goarch := runtime.GOOS, runtime.GOARCH
	ver := strings.TrimPrefix(tag, "v")
	var buf bytes.Buffer
	var name string
	if goos == "windows" {
		name = fmt.Sprintf("vibexp_%s_%s_%s.zip", ver, goos, goarch)
		zw := zip.NewWriter(&buf)
		f, err := zw.Create(binaryEntryName())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(content))
		_ = zw.Close()
	} else {
		name = fmt.Sprintf("vibexp_%s_%s_%s.tar.gz", ver, goos, goarch)
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)
		_ = tw.WriteHeader(&tar.Header{Name: binaryEntryName(), Mode: 0o755, Size: int64(len(content))})
		_, _ = tw.Write([]byte(content))
		_ = tw.Close()
		_ = gw.Close()
	}
	return buf.Bytes(), name
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// releaseServer serves the latest-release JSON plus the archive and checksums.
// badChecksum corrupts the recorded hash to exercise the mismatch path.
func releaseServer(t *testing.T, tag, content string, badChecksum bool) *httptest.Server {
	t.Helper()
	archive, assetName := makeArchive(t, tag, content)
	sum := sha256Hex(archive)
	if badChecksum {
		sum = strings.Repeat("0", 64)
	}
	checksums := fmt.Sprintf("%s  %s\n", sum, assetName)

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/repos/vibexp/cli/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"e"`)
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`,
			tag, assetName, base+"/dl/"+assetName, base+"/dl/checksums.txt")
	})
	mux.HandleFunc("/dl/"+assetName, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	srv := httptest.NewServer(mux)
	base = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

func TestSelectAssets(t *testing.T) {
	rel := &release{TagName: "v1.0.0"}
	rel.Assets = []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	}{
		{"vibexp_1.0.0_linux_amd64.tar.gz", "u/linux"},
		{"vibexp_1.0.0_windows_amd64.zip", "u/windows"},
		{"checksums.txt", "u/sums"},
	}
	bin, sum, name, err := selectAssets(rel, "linux", "amd64")
	if err != nil || bin != "u/linux" || sum != "u/sums" || name != "vibexp_1.0.0_linux_amd64.tar.gz" {
		t.Fatalf("linux select wrong: bin=%q sum=%q name=%q err=%v", bin, sum, name, err)
	}
	if _, _, _, err := selectAssets(rel, "linux", "arm64"); err == nil {
		t.Error("missing arch asset should error")
	}
}

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	content := []byte("archive bytes")
	if err := os.WriteFile(archive, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sums := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(sums, []byte(sha256Hex(content)+"  a.tar.gz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum(archive, sums, "a.tar.gz"); err != nil {
		t.Errorf("valid checksum should pass: %v", err)
	}
	// Corrupt the checksum → mismatch.
	_ = os.WriteFile(sums, []byte(strings.Repeat("0", 64)+"  a.tar.gz\n"), 0o600)
	if err := verifyChecksum(archive, sums, "a.tar.gz"); err == nil {
		t.Error("mismatched checksum should error")
	}
}

func TestExtractBinaryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	archive, _ := makeArchive(t, "v1.0.0", "BINARY-CONTENT")
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	arcPath := filepath.Join(dir, "a"+ext)
	if err := os.WriteFile(arcPath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out")
	if err := extractBinary(arcPath, binaryEntryName(), dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "BINARY-CONTENT" {
		t.Errorf("extracted content = %q", got)
	}
}

// TestApplyEndToEnd runs the full download → verify → extract → swap pipeline
// against a stub server, replacing an injected temp "binary".
func TestApplyEndToEnd(t *testing.T) {
	srv := releaseServer(t, "v9.9.9", "NEW-BINARY", false)

	target := filepath.Join(t.TempDir(), "vibexp")
	if err := os.WriteFile(target, []byte("OLD-BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	withStubs(t, "binary", target)

	var buf bytes.Buffer
	err := Apply(context.Background(), &buf, func(string) string { return "" }, srv.Client(), srv.URL, "v1.0.0", false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "NEW-BINARY" {
		t.Errorf("binary not replaced: %q", got)
	}
	if !strings.Contains(buf.String(), "Updated vibexp") {
		t.Errorf("expected success message: %q", buf.String())
	}
}

func TestApplyChecksumMismatchLeavesBinary(t *testing.T) {
	srv := releaseServer(t, "v9.9.9", "NEW-BINARY", true) // bad checksum

	target := filepath.Join(t.TempDir(), "vibexp")
	if err := os.WriteFile(target, []byte("OLD-BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	withStubs(t, "binary", target)

	var buf bytes.Buffer
	err := Apply(context.Background(), &buf, func(string) string { return "" }, srv.Client(), srv.URL, "v1.0.0", false)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "OLD-BINARY" {
		t.Errorf("binary must be untouched on mismatch, got %q", got)
	}
}

func TestApplyRefusesNonReleaseInstall(t *testing.T) {
	srv := releaseServer(t, "v9.9.9", "NEW", false)
	target := filepath.Join(t.TempDir(), "vibexp")
	_ = os.WriteFile(target, []byte("OLD"), 0o755)
	withStubs(t, "brew", target)

	var buf bytes.Buffer
	if err := Apply(context.Background(), &buf, func(string) string { return "" }, srv.Client(), srv.URL, "v1.0.0", false); err != nil {
		t.Fatalf("refuse path should not error: %v", err)
	}
	if !strings.Contains(buf.String(), "brew upgrade vibexp") {
		t.Errorf("should print brew upgrade command: %q", buf.String())
	}
	if got, _ := os.ReadFile(target); string(got) != "OLD" {
		t.Errorf("binary must be untouched: %q", got)
	}
}

func TestApplyAlreadyUpToDateAndCheckOnly(t *testing.T) {
	srv := releaseServer(t, "v1.0.0", "SAME", false)
	target := filepath.Join(t.TempDir(), "vibexp")
	_ = os.WriteFile(target, []byte("OLD"), 0o755)
	withStubs(t, "binary", target)

	var buf bytes.Buffer
	if err := Apply(context.Background(), &buf, func(string) string { return "" }, srv.Client(), srv.URL, "v1.0.0", false); err != nil {
		t.Fatalf("up-to-date: %v", err)
	}
	if !strings.Contains(buf.String(), "already up to date") {
		t.Errorf("expected up-to-date message: %q", buf.String())
	}

	// --check with a newer release reports without applying.
	srv2 := releaseServer(t, "v2.0.0", "NEW", false)
	buf.Reset()
	if err := Apply(context.Background(), &buf, func(string) string { return "" }, srv2.Client(), srv2.URL, "v1.0.0", true); err != nil {
		t.Fatalf("check-only: %v", err)
	}
	if !strings.Contains(buf.String(), "v1.0.0 → v2.0.0") {
		t.Errorf("check-only should report availability: %q", buf.String())
	}
	if got, _ := os.ReadFile(target); string(got) != "OLD" {
		t.Errorf("--check must not modify the binary: %q", got)
	}
}

// withStubs overrides the install-source ldflag and the self-exe resolver for a
// test, restoring both on cleanup.
func withStubs(t *testing.T, installSource, target string) {
	t.Helper()
	origSrc := version.InstallSource
	origFn := selfExePathFn
	version.InstallSource = installSource
	selfExePathFn = func() (string, error) { return target, nil }
	t.Cleanup(func() {
		version.InstallSource = origSrc
		selfExePathFn = origFn
	})
}
