package update

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The goreleaser archive name templates in .goreleaser.yaml must stay in lockstep
// with this package's asset matcher (selectAssets). Two archive families ship in
// every release:
//
//   - canonical:  vibexp_<version>_<os>_<arch>.tar.gz | .zip   (self-updatable)
//   - homebrew:   vibexp_homebrew_<version>_<os>-<arch>.tar.gz  (brew only)
//
// The homebrew archives exist solely so the Homebrew formula can install a
// binary built with InstallSource=brew. The self-updater must NEVER select one:
// its matcher keys on the "_<os>_" segment, which the homebrew name deliberately
// breaks ("_<os>-"). These tests lock that contract so a future rename of either
// side can't silently make `vibexp update` download the wrong archive.

func mustRelease(t *testing.T, names ...string) *release {
	t.Helper()
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf(`{"name":%q,"browser_download_url":%q}`, n, "https://example.test/"+n))
	}
	js := fmt.Sprintf(`{"tag_name":"v1.2.3","assets":[%s]}`, strings.Join(parts, ","))
	var rel release
	if err := json.Unmarshal([]byte(js), &rel); err != nil {
		t.Fatalf("build release fixture: %v", err)
	}
	return &rel
}

func TestSelectAssetsPrefersCanonicalOverHomebrew(t *testing.T) {
	rel := mustRelease(t,
		"vibexp_1.2.3_linux_amd64.tar.gz",
		"vibexp_homebrew_1.2.3_linux-amd64.tar.gz",
		"vibexp_1.2.3_darwin_arm64.tar.gz",
		"vibexp_homebrew_1.2.3_darwin-arm64.tar.gz",
		"vibexp_1.2.3_windows_amd64.zip",
		"checksums.txt",
	)

	for _, tc := range []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "vibexp_1.2.3_linux_amd64.tar.gz"},
		{"darwin", "arm64", "vibexp_1.2.3_darwin_arm64.tar.gz"},
		{"windows", "amd64", "vibexp_1.2.3_windows_amd64.zip"},
	} {
		bin, sum, name, err := selectAssets(rel, tc.goos, tc.goarch)
		if err != nil {
			t.Fatalf("%s/%s: selectAssets: %v", tc.goos, tc.goarch, err)
		}
		if name != tc.want {
			t.Errorf("%s/%s: selected %q, want canonical %q", tc.goos, tc.goarch, name, tc.want)
		}
		if bin != "https://example.test/"+tc.want {
			t.Errorf("%s/%s: bin url = %q", tc.goos, tc.goarch, bin)
		}
		if sum != "https://example.test/checksums.txt" {
			t.Errorf("%s/%s: sum url = %q", tc.goos, tc.goarch, sum)
		}
	}
}

func TestSelectAssetsRejectsHomebrewOnly(t *testing.T) {
	// Only the homebrew archive is present for this platform: the updater must
	// report "no asset" rather than fall back to it.
	rel := mustRelease(t,
		"vibexp_homebrew_1.2.3_linux-amd64.tar.gz",
		"checksums.txt",
	)
	if _, _, _, err := selectAssets(rel, "linux", "amd64"); err == nil {
		t.Fatal("expected no canonical asset, but a homebrew archive was selected")
	}
}
