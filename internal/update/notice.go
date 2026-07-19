package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/vibexp/cli/internal/version"
)

// noticeParams carries the dependencies of the notice core, so tests can inject
// a stub GitHub server, a fixed clock, and an in-memory state path.
type noticeParams struct {
	w         io.Writer
	getenv    func(string) string
	statePath string
	apiBase   string
	client    *http.Client
	current   string
	now       time.Time
}

// Notify runs the cached background version check and, if a newer release is
// available, prints a single upgrade notice to w. It never blocks meaningfully
// (cache hit = no network; a miss is capped at checkTimeout) and never returns
// an error — the check is strictly advisory. Called once per invocation from
// Execute after the command has produced its output.
func Notify(ctx context.Context, w io.Writer, getenv func(string) string) {
	statePath, err := DefaultStatePath()
	if err != nil {
		return
	}
	notify(ctx, noticeParams{
		w:         w,
		getenv:    getenv,
		statePath: statePath,
		apiBase:   defaultAPIBase,
		client:    &http.Client{Timeout: checkTimeout},
		current:   version.Version,
		now:       time.Now(),
	})
}

// notify is the testable core.
func notify(ctx context.Context, p noticeParams) {
	if Suppressed(p.getenv, p.current) {
		return
	}
	st := LoadState(p.statePath)

	if ShouldCheck(st, p.now) {
		reqCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		defer cancel()
		tag, etag, notModified, err := fetchLatest(reqCtx, p.client, p.apiBase, st.ETag)
		// Record the attempt regardless, so a failing/slow endpoint is still
		// rate-limited to once per interval.
		st.LastCheck = p.now
		if err == nil && !notModified {
			st.LatestSeen = tag
			st.ETag = etag
		}
		_ = SaveState(p.statePath, st) // best effort
	}

	if st.LatestSeen == "" || !IsNewer(p.current, st.LatestSeen) {
		return
	}
	printNotice(p.w, p.getenv, p.current, st.LatestSeen)
}

// printNotice writes the single-line (plus hint) upgrade notice, tailored to how
// the binary was installed.
func printNotice(w io.Writer, getenv func(string) string, current, latest string) {
	src := DetectSource(getenv)
	hint := "run: vibexp update"
	if !src.SelfUpdatable() {
		hint = "run: " + src.UpgradeCommand()
	}
	fmt.Fprintf(w, "\nA new release of vibexp is available: %s → %s\n%s\n", normalize(current), normalize(latest), hint)
}
