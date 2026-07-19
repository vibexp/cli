package api

import (
	"strings"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

// Team returns the resolved team id or slug for a team-scoped command. The
// precedence (--team > VIBEXP_TEAM > active-context default) is already applied
// in config.Resolve; a UUID or slug is passed through unchanged. A missing team
// is a usage error (exit 2) naming all three ways to set it.
func Team(rt *config.Runtime) (string, error) {
	if v := strings.TrimSpace(rt.Team); v != "" {
		return v, nil
	}
	return "", exitcode.Usage("no team set: pass --team <id|slug>, set VIBEXP_TEAM, or set a default on the context (vibexp config set-context %s --team <id|slug>)", contextName(rt))
}

// Project returns the resolved project id or slug, same precedence and error
// contract as Team.
func Project(rt *config.Runtime) (string, error) {
	if v := strings.TrimSpace(rt.Project); v != "" {
		return v, nil
	}
	return "", exitcode.Usage("no project set: pass --project <id|slug>, set VIBEXP_PROJECT, or set a default on the context (vibexp config set-context %s --project <id|slug>)", contextName(rt))
}

func contextName(rt *config.Runtime) string {
	if rt.ContextName == "" {
		return "<name>"
	}
	return rt.ContextName
}
