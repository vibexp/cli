package resource

import "strings"

// Every list endpoint returns its array under a noun-specific key, but not at a
// consistent depth: most put it at the top level ({"memories":[…]}), while
// listPrompts nests it under an envelope ({"data":{"prompts":[…]},"message",
// "status"}). The row expression therefore has to try both, and each noun used
// to spell that out itself — which is how #72 happened.
//
// The rule that matters, and the reason this is declared once: EVERY
// alternative must index a named key. A bare `.data[]?` catch-all looks
// harmless but is not, because `[]?` over an *object* iterates its values — so
// an unrecognised envelope rendered one blank row per envelope field (page,
// per_page, total_count, …) instead of nothing. Worse, gojq's `//` falls
// through whenever its left side yields *no* values, so an **empty** list hit
// that catch-all too and wrote phantom rows to stdout where scripts count
// lines.
//
// Keeping every alternative key-indexed means an envelope we do not recognise
// renders zero rows rather than garbage.

// ListRows builds the row expression for a list view from the endpoint's array
// key(s), e.g. ListRows("memories") matches both {"memories":[…]} and
// {"data":{"memories":[…]}}. Extra keys are tried in the order given, for the
// endpoints that spell the array more than one way. The generic `items` alias
// is always appended last.
//
// Use this for every multi-row list spec; do not hand-write the expression.
func ListRows(keys ...string) string {
	var alts []string
	seen := map[string]bool{}
	add := func(key string) {
		if seen[key] {
			return
		}
		seen[key] = true
		alts = append(alts, "."+key+"[]?", ".data."+key+"[]?")
	}
	for _, key := range keys {
		add(key)
	}
	add("items")
	return strings.Join(alts, " // ")
}
