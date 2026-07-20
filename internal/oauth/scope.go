package oauth

import "strings"

// NegotiateScopes decides which scopes to request on the authorization request.
//
// An explicit override (from a flag or env var) wins outright and is returned
// verbatim (trimmed and de-duplicated) — it is the escape hatch for deployments
// whose authorization server uses non-standard scope names that negotiation
// could not discover.
//
// Otherwise the result is the intersection of the CLI's preferred scopes with
// the server-advertised scopes_supported (RFC 8414), preserving preferred
// order. This means an unadvertised scope is never requested — some
// authorization servers reject an unknown scope with invalid_scope. An empty
// result (nothing preferred is advertised, or the server advertises nothing)
// tells the flow to omit the scope parameter entirely.
func NegotiateScopes(preferred, supported, override []string) []string {
	if len(override) > 0 {
		return cleanScopes(override)
	}
	if len(supported) == 0 {
		return nil
	}
	advertised := make(map[string]struct{}, len(supported))
	for _, s := range supported {
		advertised[s] = struct{}{}
	}
	var out []string
	for _, p := range preferred {
		if _, ok := advertised[p]; ok {
			out = append(out, p)
		}
	}
	return out
}

// cleanScopes trims each scope and drops empties and duplicates while keeping
// first-seen order.
func cleanScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	var out []string
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
