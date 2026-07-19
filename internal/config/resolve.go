package config

import "time"

// Environment variable names honored during resolution.
const (
	EnvContext = "VIBEXP_CONTEXT"
	EnvBaseURL = "VIBEXP_BASE_URL"
	EnvTeam    = "VIBEXP_TEAM"
	EnvProject = "VIBEXP_PROJECT"
)

// Overrides carries values sourced from command-line flags. Empty strings mean
// "not set" and fall through to the next precedence level.
type Overrides struct {
	Context string
	BaseURL string
	Team    string
	Project string
	Timeout time.Duration
}

// Runtime is the fully-resolved effective configuration handed to commands.
type Runtime struct {
	// ContextName is the name of the active context (may be empty if none).
	ContextName string
	BaseURL     string
	Team        string
	Project     string
	Timeout     time.Duration
}

// Getenv is the signature of an environment lookup (os.Getenv), injectable for
// tests.
type Getenv func(string) string

// firstNonEmpty returns the first non-empty argument.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Resolve computes the effective Runtime using precedence flag > env > active
// context, per field. The active context is chosen by the same precedence
// (flag --context > VIBEXP_CONTEXT > CurrentContext).
func (f *File) Resolve(o Overrides, env Getenv) Runtime {
	if env == nil {
		env = func(string) string { return "" }
	}

	ctxName := firstNonEmpty(o.Context, env(EnvContext), f.CurrentContext)

	var active Context
	if c := f.Find(ctxName); c != nil {
		active = *c
	}

	return Runtime{
		ContextName: ctxName,
		BaseURL:     firstNonEmpty(o.BaseURL, env(EnvBaseURL), active.BaseURL),
		Team:        firstNonEmpty(o.Team, env(EnvTeam), active.DefaultTeam),
		Project:     firstNonEmpty(o.Project, env(EnvProject), active.DefaultProject),
		Timeout:     o.Timeout,
	}
}
