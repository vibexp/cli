package resource

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/exitcode"
)

// MetadataFilter is the opt-in metadata filter for list commands: repeatable
// --metadata key=value pairs merged into the server's JSON containment param
// (keys ANDed, values within a key ORed). With Tags enabled, --tags <tag> is
// sugar for metadata.tags=<tag> (memories only — the platform stores tags at
// metadata.tags; platform v0.9.0, epic #519).
type MetadataFilter struct {
	pairs []string
	tags  []string
	// Tags exposes --tags on the command (memory list only).
	Tags bool
}

// AddMetadataFlags binds --metadata (and --tags when f.Tags) to cmd.
func AddMetadataFlags(cmd *cobra.Command, f *MetadataFilter) {
	cmd.Flags().StringArrayVar(&f.pairs, "metadata", nil,
		"filter by metadata as key=value (repeatable; keys AND, values within a key OR)")
	if f.Tags {
		cmd.Flags().StringArrayVar(&f.tags, "tags", nil,
			"filter by tag (repeatable; sugar for metadata.tags=<tag>)")
	}
}

// Query returns the metadata containment value as JSON, or "" when unset.
// Keys are sorted for a deterministic query string. An empty value
// (--metadata key=) means "the key exists".
func (f *MetadataFilter) Query() (string, error) {
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

// ApplyToPath merges the metadata filter into a path's query string; a no-op
// when the filter is unset.
func (f *MetadataFilter) ApplyToPath(path string) (string, error) {
	meta, err := f.Query()
	if err != nil {
		return "", err
	}
	if meta == "" {
		return path, nil
	}
	u, err := url.Parse(path)
	if err != nil {
		return "", exitcode.Usage("invalid path %q: %v", path, err)
	}
	q := u.Query()
	q.Set("metadata", meta)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
