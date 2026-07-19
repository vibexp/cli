package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vibexp/cli/internal/version"
)

// Source identifies how the running binary was installed, which decides whether
// `vibexp update` may self-replace it.
type Source int

const (
	// SourceBinary is a GitHub Releases download (goreleaser). Self-updatable.
	SourceBinary Source = iota
	// SourceBrew is a Homebrew install — upgrade via brew.
	SourceBrew
	// SourceGoInstall is a `go install` / source build — upgrade via go install.
	SourceGoInstall
)

// SelfUpdatable reports whether `vibexp update` may replace this binary.
func (s Source) SelfUpdatable() bool { return s == SourceBinary }

// UpgradeCommand returns the correct upgrade instruction for a non-self-updatable
// source.
func (s Source) UpgradeCommand() string {
	switch s {
	case SourceBrew:
		return "brew upgrade vibexp"
	default:
		return "go install github.com/vibexp/cli/cmd/vibexp@latest"
	}
}

// DetectSource classifies the install source: the InstallSource ldflag (set by
// goreleaser/brew) is authoritative; absent it, a path heuristic on the
// executable distinguishes Homebrew and go-install layouts, defaulting to
// go-install (a source build is upgraded the same way).
func DetectSource(getenv func(string) string) Source {
	switch strings.ToLower(strings.TrimSpace(version.InstallSource)) {
	case "binary", "release", "goreleaser":
		return SourceBinary
	case "brew", "homebrew":
		return SourceBrew
	case "goinstall", "go-install":
		return SourceGoInstall
	}
	return detectSourceByPath(getenv, os.Executable)
}

// detectSourceByPath applies the path heuristic (extracted for tests).
func detectSourceByPath(getenv func(string) string, executable func() (string, error)) Source {
	exe, err := executable()
	if err != nil {
		return SourceGoInstall
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	lower := strings.ToLower(filepath.ToSlash(exe))

	// Homebrew installs live under a Cellar / homebrew prefix.
	if strings.Contains(lower, "/cellar/") || strings.Contains(lower, "/homebrew/") {
		return SourceBrew
	}
	// go install drops binaries in GOBIN or $GOPATH/bin (default ~/go/bin).
	for _, dir := range goBinDirs(getenv) {
		if dir != "" && strings.HasPrefix(exe, dir) {
			return SourceGoInstall
		}
	}
	return SourceGoInstall
}

// goBinDirs returns the candidate go-install output directories.
func goBinDirs(getenv func(string) string) []string {
	var dirs []string
	if gobin := getenv("GOBIN"); gobin != "" {
		dirs = append(dirs, gobin)
	}
	if gopath := getenv("GOPATH"); gopath != "" {
		for _, p := range filepath.SplitList(gopath) {
			dirs = append(dirs, filepath.Join(p, "bin"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	return dirs
}

// assetOS/assetArch expose the target platform for asset naming (overridable in
// tests via the exported helpers below).
func assetOS() string   { return runtime.GOOS }
func assetArch() string { return runtime.GOARCH }
