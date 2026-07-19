package logging

import (
	"log/slog"
	"regexp"
	"strings"
	"sync"
)

// placeholder is substituted for any redacted value.
const placeholder = "***REDACTED***"

// sensitiveKey matches attribute keys whose values must always be redacted,
// regardless of content.
var sensitiveKey = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|authorization|password|credential|bearer)`)

// registry holds exact secret values registered at runtime (API keys, tokens).
// Every credential the CLI handles is registered here so it is scrubbed even
// when it appears inside an otherwise-innocent message or nested value.
var (
	registryMu sync.RWMutex
	registry   = map[string]struct{}{}
)

// RegisterSecret records a secret value so it is redacted anywhere it appears
// in log output. Short/empty values are ignored to avoid corrupting logs.
func RegisterSecret(secret string) {
	if len(secret) < 4 {
		return
	}
	registryMu.Lock()
	registry[secret] = struct{}{}
	registryMu.Unlock()
}

// Redact replaces every registered secret substring in s with the placeholder.
func Redact(s string) string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if len(registry) == 0 {
		return s
	}
	for secret := range registry {
		if secret != "" && strings.Contains(s, secret) {
			s = strings.ReplaceAll(s, secret, placeholder)
		}
	}
	return s
}

// redactAttr is the slog ReplaceAttr hook. It redacts by key (sensitive names)
// and by value (registered secret substrings), recursing into groups.
func redactAttr(_ []string, a slog.Attr) slog.Attr {
	if sensitiveKey.MatchString(a.Key) {
		a.Value = slog.StringValue(placeholder)
		return a
	}
	if a.Value.Kind() == slog.KindString {
		a.Value = slog.StringValue(Redact(a.Value.String()))
	}
	return a
}
