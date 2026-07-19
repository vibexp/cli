package oauth

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Headless reports whether the environment has no usable browser (so the CLI
// should fail fast toward API-key auth). VIBEXP_NO_BROWSER forces headless.
func Headless(getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	if getenv("VIBEXP_NO_BROWSER") != "" {
		return true
	}
	if runtime.GOOS == "linux" {
		// No display server → no browser (typical for SSH / servers).
		return getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == ""
	}
	return false
}

// OpenBrowser opens rawURL in the platform's default browser. The launcher is
// invoked by absolute path (never resolved through $PATH) so a hijacked PATH
// cannot substitute the browser opener.
func OpenBrowser(rawURL string) error {
	bin, args, err := browserCommand(rawURL)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}
	// Reap the process so it does not linger as a zombie; ignore its exit.
	go func() { _ = cmd.Wait() }()
	return nil
}

// browserCommand resolves the absolute launcher path and arguments for the
// current OS. It returns an error when no known launcher exists (callers fall
// back to printing the URL for manual opening).
func browserCommand(rawURL string) (string, []string, error) {
	switch runtime.GOOS {
	case "darwin":
		return firstExisting([]string{"/usr/bin/open"}, []string{rawURL})
	case "windows":
		sysRoot := os.Getenv("SystemRoot")
		if sysRoot == "" {
			sysRoot = `C:\Windows`
		}
		return filepath.Join(sysRoot, "System32", "rundll32.exe"),
			[]string{"url.dll,FileProtocolHandler", rawURL}, nil
	default:
		return firstExisting([]string{"/usr/bin/xdg-open", "/bin/xdg-open"}, []string{rawURL})
	}
}

// firstExisting returns the first candidate path that exists on disk.
func firstExisting(candidates []string, args []string) (string, []string, error) {
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, args, nil
		}
	}
	return "", nil, fmt.Errorf("no browser launcher found (tried %v)", candidates)
}
