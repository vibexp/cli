// Package version exposes build metadata injected at link time via -ldflags -X.
package version

// These variables are overridden at build time. Defaults are chosen so a plain
// `go build` / `go install` (no ldflags) still produces sensible output.
var (
	// Version is the semantic version of the CLI (e.g. "v1.2.3").
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "none"
	// Date is the build timestamp (RFC 3339).
	Date = "unknown"
	// InstallSource records how the binary was distributed ("source", "brew",
	// "goreleaser", "go-install"). Later issues (#15/#16) gate self-update on it.
	InstallSource = "source"
)

// Info bundles the build metadata for rendering.
type Info struct {
	Version       string `json:"version" yaml:"version"`
	Commit        string `json:"commit" yaml:"commit"`
	Date          string `json:"date" yaml:"date"`
	InstallSource string `json:"install_source" yaml:"install_source"`
}

// Current returns the build metadata for this binary.
func Current() Info {
	return Info{
		Version:       Version,
		Commit:        Commit,
		Date:          Date,
		InstallSource: InstallSource,
	}
}
