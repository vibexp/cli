package resource

import "testing"

// A fabricated lock, in the platform's ResourceLock shape.
const lockedDoc = `{"slug":"greet","lock":{"comment":"used by the release agent","locked_at":"2026-10-11T10:15:30Z","locked_by":{"user_id":"u-1","display_name":"Ada"}}}`

func TestLockColumnsRenderLockedAndUnlocked(t *testing.T) {
	cols := LockDetailColumns()
	wantHeaders := []string{"LOCKED", "LOCKED_BY", "LOCKED_AT", "LOCK_COMMENT"}
	if len(cols) != len(wantHeaders) {
		t.Fatalf("detail columns = %d, want %d", len(cols), len(wantHeaders))
	}
	wantLocked := []any{"locked", "Ada", "2026-10-11T10:15:30Z", "used by the release agent"}
	for i, c := range cols {
		if c.Header != wantHeaders[i] {
			t.Errorf("column %d header = %q, want %q", i, c.Header, wantHeaders[i])
		}
		if got := runPath(t, c.Path, lockedDoc); got != wantLocked[i] {
			t.Errorf("%s on a locked resource = %v, want %v", c.Header, got, wantLocked[i])
		}
	}
	// Unlocked: the platform sends "lock": null (and older servers omit it).
	// The flag must be an empty string, never false or null text.
	for _, doc := range []string{`{"slug":"greet","lock":null}`, `{"slug":"greet"}`} {
		if got := runPath(t, LockColumn().Path, doc); got != "" {
			t.Errorf("LOCKED on %s = %#v, want empty", doc, got)
		}
		for _, c := range cols[1:] {
			if got := runPath(t, c.Path, doc); got != nil {
				t.Errorf("%s on %s = %#v, want nil (empty cell)", c.Header, doc, got)
			}
		}
	}
}
