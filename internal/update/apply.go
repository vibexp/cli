package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxAssetBytes caps a downloaded asset (defensive; release binaries are small).
const maxAssetBytes = 512 << 20 // 512 MiB

// Apply performs `vibexp update`: it refuses on non-self-updatable installs
// (printing the correct command), otherwise downloads the matching release
// asset, verifies it against the release checksums, and atomically replaces the
// running binary. checkOnly reports availability without applying.
func Apply(ctx context.Context, w io.Writer, getenv func(string) string, client *http.Client, apiBase, current string, checkOnly bool) error {
	src := DetectSource(getenv)
	if !src.SelfUpdatable() {
		fmt.Fprintf(w, "vibexp was not installed from GitHub Releases, so it cannot self-update.\nUpgrade with:\n  %s\n", src.UpgradeCommand())
		return nil
	}

	rel, err := fetchRelease(ctx, client, apiBase)
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}
	if rel.TagName == "" {
		return errors.New("no published release found")
	}
	if !IsNewer(current, rel.TagName) {
		fmt.Fprintf(w, "vibexp is already up to date (%s).\n", normalize(current))
		return nil
	}
	if checkOnly {
		fmt.Fprintf(w, "A new release is available: %s → %s\nRun `vibexp update` to install it.\n", normalize(current), normalize(rel.TagName))
		return nil
	}

	binURL, sumURL, assetName, err := selectAssets(rel, assetOS(), assetArch())
	if err != nil {
		return err
	}

	// Resolve and probe the target early: fail before downloading if we can't
	// write it (e.g. a root-owned binary), leaving nothing behind.
	target, err := selfExePathFn()
	if err != nil {
		return err
	}
	if err := probeWritable(target); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "vibexp-update-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, assetName)
	if err := download(ctx, client, binURL, archivePath); err != nil {
		return fmt.Errorf("download %s: %w", assetName, err)
	}
	sumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := download(ctx, client, sumURL, sumsPath); err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	if err := verifyChecksum(archivePath, sumsPath, assetName); err != nil {
		return err // original binary untouched
	}

	newBin := filepath.Join(tmpDir, "vibexp-new")
	if err := extractBinary(archivePath, binaryEntryName(), newBin); err != nil {
		return err
	}
	if err := replaceExecutable(target, newBin); err != nil {
		return err
	}

	fmt.Fprintf(w, "Updated vibexp %s → %s.\n", normalize(current), normalize(rel.TagName))
	confirmVersion(ctx, w, target)
	return nil
}

// fetchRelease fetches the latest release with its assets (no ETag/caching —
// this is the explicit update path).
func fetchRelease(ctx context.Context, client *http.Client, apiBase string) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/repos/"+releaseRepo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "vibexp-cli")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("no published releases found for " + releaseRepo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases: unexpected status %d", resp.StatusCode)
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// selectAssets picks the archive matching this OS/arch and the checksums file
// from the release's asset list. It matches on the OS/arch/extension contract
// rather than an exact version string, so it stays robust to version formatting.
func selectAssets(rel *release, goos, goarch string) (binURL, sumURL, assetName string, err error) {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	for _, a := range rel.Assets {
		name := strings.ToLower(a.Name)
		switch {
		case strings.Contains(name, "checksums") && strings.HasSuffix(name, ".txt"):
			sumURL = a.BrowserDownloadURL
		case strings.Contains(name, "_"+goos+"_") && strings.Contains(name, goarch) && strings.HasSuffix(name, ext):
			binURL, assetName = a.BrowserDownloadURL, a.Name
		}
	}
	if binURL == "" {
		return "", "", "", fmt.Errorf("no release asset for %s/%s (expected a *_%s_%s*%s)", goos, goarch, goos, goarch, ext)
	}
	if sumURL == "" {
		return "", "", "", errors.New("release has no checksums.txt asset")
	}
	return binURL, sumURL, assetName, nil
}

// download streams url to dest.
func download(ctx context.Context, client *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "vibexp-cli")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, io.LimitReader(resp.Body, maxAssetBytes))
	return err
}

// verifyChecksum recomputes the archive's SHA-256 and compares it against the
// entry for assetName in a goreleaser-style checksums file ("<hex>  <name>").
func verifyChecksum(archivePath, sumsPath, assetName string) error {
	want, err := checksumFor(sumsPath, assetName)
	if err != nil {
		return err
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s (want %s, got %s); binary left unchanged", assetName, want, got)
	}
	return nil
}

// checksumFor finds assetName's expected hash in a checksums file.
func checksumFor(sumsPath, assetName string) (string, error) {
	data, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && filepath.Base(fields[1]) == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum entry for %s", assetName)
}

// extractBinary writes the archive entry named entry to dest (executable),
// handling tar.gz and zip.
func extractBinary(archivePath, entry, dest string) error {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractZip(archivePath, entry, dest)
	}
	return extractTarGz(archivePath, entry, dest)
}

func extractTarGz(archivePath, entry, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("archive has no %q entry", entry)
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) == entry {
			return writeExecutable(dest, io.LimitReader(tr, maxAssetBytes))
		}
	}
}

func extractZip(archivePath, entry, dest string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()
	for _, zf := range zr.File {
		if filepath.Base(zf.Name) == entry {
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			defer func() { _ = rc.Close() }()
			return writeExecutable(dest, io.LimitReader(rc, maxAssetBytes))
		}
	}
	return fmt.Errorf("archive has no %q entry", entry)
}

func writeExecutable(dest string, r io.Reader) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// selfExePathFn resolves the binary to replace; a package var so tests can point
// the swap at a temporary file instead of the real test binary.
var selfExePathFn = selfExePath

// selfExePath resolves the running binary's real path (following symlinks).
func selfExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// probeWritable checks we can create files in the target's directory, failing
// early with a clear message before any download.
func probeWritable(target string) error {
	dir := filepath.Dir(target)
	f, err := os.CreateTemp(dir, ".vibexp-write-probe-*")
	if err != nil {
		return fmt.Errorf("cannot update %s (directory not writable): %w", target, err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

// replaceExecutable atomically swaps newBin into target. It moves the running
// binary aside first (so the swap works even on Windows, where an open file
// cannot be overwritten) and rolls back on failure.
func replaceExecutable(target, newBin string) error {
	// Stage the new binary in the target directory so the final rename is on the
	// same filesystem (atomic).
	staged := target + ".new"
	if err := copyFile(newBin, staged, 0o755); err != nil {
		return err
	}
	backup := target + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("move current binary aside: %w", err)
	}
	if err := os.Rename(staged, target); err != nil {
		_ = os.Rename(backup, target) // roll back
		_ = os.Remove(staged)
		return fmt.Errorf("install new binary: %w", err)
	}
	_ = os.Remove(backup) // best effort (may be locked on Windows; harmless)
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// binaryEntryName is the binary's name inside the release archive.
func binaryEntryName() string {
	if assetOS() == "windows" {
		return "vibexp.exe"
	}
	return "vibexp"
}

// confirmVersion re-executes the freshly installed binary to report its version,
// confirming the swap took effect. A failure here is non-fatal (the update
// already succeeded).
func confirmVersion(ctx context.Context, w io.Writer, target string) {
	out, err := exec.CommandContext(ctx, target, "version").CombinedOutput()
	if err != nil {
		return
	}
	fmt.Fprintf(w, "Now running: %s", out)
}
