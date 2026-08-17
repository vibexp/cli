package resource

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/exitcode"
)

// ListFilters is the opt-in server-side filter set for a list command. Each
// exported field turns one flag on, because the four curated nouns wrap
// endpoints that accept different filters — binding a flag the endpoint ignores
// would let a user narrow a list and get the full one back, which reads like a
// legitimate answer.
//
//   - Metadata → --metadata key=value (repeatable), merged into the server's
//     JSON containment param: keys ANDed, values within a key ORed.
//   - Tags → --tags <tag> (repeatable), sugar for metadata.tags=<tag>. Memories
//     only; the platform stores tags at metadata.tags (v0.9.0, epic #519).
//   - Stale → --stale, mapping to freshness=stale (v0.11.0, epic #726).
type ListFilters struct {
	// Metadata exposes --metadata (every noun whose endpoint takes it — not
	// prompts, whose listPrompts has no metadata param).
	Metadata bool
	// Tags exposes --tags (memory list only).
	Tags bool
	// Stale exposes --stale.
	Stale bool

	pairs []string
	tags  []string
	stale bool
}

// AddFilterFlags binds the flags f opted into onto cmd.
func AddFilterFlags(cmd *cobra.Command, f *ListFilters) {
	if f.Metadata {
		cmd.Flags().StringArrayVar(&f.pairs, "metadata", nil,
			"filter by metadata as key=value (repeatable; keys AND, values within a key OR)")
	}
	if f.Tags {
		cmd.Flags().StringArrayVar(&f.tags, "tags", nil,
			"filter by tag (repeatable; sugar for metadata.tags=<tag>)")
	}
	if f.Stale {
		cmd.Flags().BoolVar(&f.stale, "stale", false,
			"only resources the team's freshness rules have flagged as stale")
	}
}

// Query returns the metadata containment value as JSON, or "" when unset.
// Keys are sorted for a deterministic query string. An empty value
// (--metadata key=) means "the key exists".
func (f *ListFilters) Query() (string, error) {
	merged := map[string][]string{}
	for _, p := range f.pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return "", exitcode.Usage("invalid --metadata %q: expected key=value", p)
		}
		merged[k] = append(merged[k], v)
	}
	if len(f.tags) > 0 {
		merged["tags"] = append(merged["tags"], f.tags...)
	}
	if len(merged) == 0 {
		return "", nil
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(merged[k])
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.String(), nil
}

// ApplyToPath merges every set filter into a path's query string; a no-op when
// none is set, so an unfiltered list keeps its path byte-for-byte (in
// particular it never grows an empty freshness=).
func (f *ListFilters) ApplyToPath(path string) (string, error) {
	meta, err := f.Query()
	if err != nil {
		return "", err
	}
	if meta == "" && !f.stale {
		return path, nil
	}
	u, err := url.Parse(path)
	if err != nil {
		return "", exitcode.Usage("invalid path %q: %v", path, err)
	}
	q := u.Query()
	if meta != "" {
		q.Set("metadata", meta)
	}
	if f.stale {
		// The server models this as a single-member enum so a future state can
		// be added without a type change; anything but "stale" is a 400.
		q.Set("freshness", "stale")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
