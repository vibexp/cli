package oauth

import (
	"fmt"
	"os"
	"os/exec"
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

// OpenBrowser opens rawURL in the platform's default browser.
func OpenBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}
	// Reap the process so it does not linger as a zombie; ignore its exit.
	go func() { _ = cmd.Wait() }()
	return nil
}
