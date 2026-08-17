package resource

import "github.com/vibexp/cli/internal/output"

// The v0.11.0 resource freshness state (platform epic #726) lives on every
// curated resource — prompts, memories, blueprints and artifacts — so its
// columns are declared once here rather than copied into each noun package.
// That matters most for the STALE_RULES guard below, which is easy to get
// subtly wrong and would otherwise need fixing in four places.
//
// The object is present ONLY while a resource is stale; absence means fresh.
// Every column therefore renders an empty cell on a fresh resource, which the
// renderer already does for a missing path (see internal/output/extract.go).

// FreshnessColumn is the compact list-view flag: "stale", or empty when fresh.
// Place it immediately before the UPDATED column, which it qualifies.
func FreshnessColumn() output.Column {
	return output.Column{Header: "STALE", Path: ".freshness.status"}
}

// FreshnessDetailColumns is the full state for single-object views: the flag
// plus when it was first flagged, why, and how many rules matched. The raw
// matched_rule_ids stay available through --format=json; there is no CLI-side
// rule lookup to resolve them to names.
func FreshnessDetailColumns() []output.Column {
	return []output.Column{
		FreshnessColumn(),
		{Header: "STALE_SINCE", Path: ".freshness.since"},
		{Header: "STALE_REASON", Path: ".freshness.reason"},
		// Guarded rather than a bare `| length`: in jq `null | length` is 0, so
		// an unguarded path would render "0" on every fresh resource — reading
		// as "evaluated, no rules matched" instead of "not stale".
		{Header: "STALE_RULES", Path: `if .freshness then (.freshness.matched_rule_ids | length | tostring) else "" end`},
	}
}

// WithFreshnessDetail splices the freshness detail columns between head and
// tail, so a noun's detail spec reads as its own fields with the shared
// freshness block dropped in at the right position.
func WithFreshnessDetail(head, tail []output.Column) []output.Column {
	fresh := FreshnessDetailColumns()
	out := make([]output.Column, 0, len(head)+len(fresh)+len(tail))
	out = append(out, head...)
	out = append(out, fresh...)
	out = append(out, tail...)
	return out
}
