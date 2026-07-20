// Package version exposes build metadata injected at link time via -ldflags -X.
package version

import "runtime/debug"

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

// Current returns the build metadata for this binary. When the ldflags were not
// applied — chiefly `go install github.com/vibexp/cli/cmd/vibexp@<tag>` — it
// falls back to the module version and VCS stamps embedded by the Go toolchain
// (debug.ReadBuildInfo), so the version is still reported correctly.
func Current() Info {
	bi, ok := debug.ReadBuildInfo()
	v, c, d := resolve(Version, Commit, Date, bi, ok)
	return Info{
		Version:       v,
		Commit:        c,
		Date:          d,
		InstallSource: InstallSource,
	}
}

// resolve applies the debug.ReadBuildInfo fallback to the raw ldflag values. It
// only fills a field that is still at its default, so an explicit ldflag
// (goreleaser / the Makefile) always wins. Extracted for testing.
func resolve(rawVersion, rawCommit, rawDate string, bi *debug.BuildInfo, ok bool) (version, commit, date string) {
	version, commit, date = rawVersion, rawCommit, rawDate
	if !ok || bi == nil {
		return version, commit, date
	}

	// `go install …@v1.2.3` records the tag as the main module version; a local
	// build records "(devel)", which carries no useful version.
	if version == "dev" {
		if mv := bi.Main.Version; mv != "" && mv != "(devel)" {
			version = mv
		}
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "none" && s.Value != "" {
				commit = s.Value
			}
		case "vcs.time":
			if date == "unknown" && s.Value != "" {
				date = s.Value
			}
		}
	}
	return version, commit, date
}
