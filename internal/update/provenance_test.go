package update

import (
	"testing"

	"github.com/vibexp/cli/internal/version"
)

func TestDetectSourceFromLdflag(t *testing.T) {
	orig := version.InstallSource
	defer func() { version.InstallSource = orig }()

	cases := map[string]Source{
		"binary": SourceBinary,
		"brew":   SourceBrew,
	}
	for ldflag, want := range cases {
		version.InstallSource = ldflag
		if got := DetectSource(func(string) string { return "" }); got != want {
			t.Errorf("InstallSource=%q → %v, want %v", ldflag, got, want)
		}
	}
}

func TestDetectSourceByPath(t *testing.T) {
	getenv := func(k string) string {
		if k == "GOPATH" {
			return "/home/dev/go"
		}
		return ""
	}
	cases := []struct {
		name string
		exe  string
		want Source
	}{
		{"homebrew cellar", "/opt/homebrew/Cellar/vibexp/1.0.0/bin/vibexp", SourceBrew},
		{"gopath bin", "/home/dev/go/bin/vibexp", SourceGoInstall},
		{"random path", "/usr/local/bin/vibexp", SourceGoInstall},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exe := tc.exe
			got := detectSourceByPath(getenv, func() (string, error) { return exe, nil })
			if got != tc.want {
				t.Errorf("path %q → %v, want %v", tc.exe, got, tc.want)
			}
		})
	}
}

func TestUpgradeCommand(t *testing.T) {
	if !SourceBinary.SelfUpdatable() {
		t.Error("binary install should be self-updatable")
	}
	if SourceBrew.SelfUpdatable() || SourceGoInstall.SelfUpdatable() {
		t.Error("brew/go-install must not be self-updatable")
	}
	if got := SourceBrew.UpgradeCommand(); got != "brew upgrade vibexp" {
		t.Errorf("brew command = %q", got)
	}
	if got := SourceGoInstall.UpgradeCommand(); got != "go install github.com/vibexp/cli/cmd/vibexp@latest" {
		t.Errorf("go-install command = %q", got)
	}
}
