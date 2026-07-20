package version

import (
	"runtime/debug"
	"testing"
)

func TestResolvePrefersLdflags(t *testing.T) {
	// Explicit ldflags (goreleaser/Makefile) must always win over BuildInfo.
	bi := &debug.BuildInfo{
		Main: debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "deadbeef"},
			{Key: "vcs.time", Value: "2020-01-01T00:00:00Z"},
		},
	}
	v, c, d := resolve("v1.2.3", "abc123", "2026-01-01T00:00:00Z", bi, true)
	if v != "v1.2.3" || c != "abc123" || d != "2026-01-01T00:00:00Z" {
		t.Errorf("ldflags should win: got %q %q %q", v, c, d)
	}
}

func TestResolveGoInstallFallback(t *testing.T) {
	// `go install …@v1.2.3`: no ldflags, module version + vcs stamps present.
	bi := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-07-20T00:00:00Z"},
		},
	}
	v, c, d := resolve("dev", "none", "unknown", bi, true)
	if v != "v1.2.3" {
		t.Errorf("version = %q, want v1.2.3 from module version", v)
	}
	if c != "0123456789abcdef" {
		t.Errorf("commit = %q, want vcs.revision", c)
	}
	if d != "2026-07-20T00:00:00Z" {
		t.Errorf("date = %q, want vcs.time", d)
	}
}

func TestResolveLocalDevelKeepsDefaults(t *testing.T) {
	// A plain local `go build` records "(devel)" — no useful version, but vcs
	// stamps still fill in commit/date.
	bi := &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "cafebabe"},
		},
	}
	v, c, d := resolve("dev", "none", "unknown", bi, true)
	if v != "dev" {
		t.Errorf("version = %q, want dev for (devel) build", v)
	}
	if c != "cafebabe" {
		t.Errorf("commit = %q, want cafebabe", c)
	}
	if d != "unknown" {
		t.Errorf("date = %q, want unknown (no vcs.time)", d)
	}
}

func TestResolveNoBuildInfo(t *testing.T) {
	v, c, d := resolve("dev", "none", "unknown", nil, false)
	if v != "dev" || c != "none" || d != "unknown" {
		t.Errorf("no build info should keep defaults: got %q %q %q", v, c, d)
	}
}
