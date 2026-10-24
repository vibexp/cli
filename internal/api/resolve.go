package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

// Team returns the resolved team id for a team-scoped command. The precedence
// (--team > VIBEXP_TEAM > active-context default) is already applied in
// config.Resolve. REST paths take only a UUID, so a slug is looked up through
// the runtime's SlugResolver (installed by the root when it builds the runtime,
// see NewSlugResolver) and the UUID is written back to rt, so later calls in
// the same invocation make no request. A missing team is a usage error (exit 2)
// naming all three ways to set it.
func Team(rt *config.Runtime) (string, error) {
	v := strings.TrimSpace(rt.Team)
	if v == "" {
		return "", exitcode.Usage("no team set: pass --team <id|slug>, set VIBEXP_TEAM, or set a default on the context (vibexp config set-context %s --team <id|slug>)", contextName(rt))
	}
	if IsUUID(v) {
		return v, nil
	}
	if rt.Slugs == nil {
		return "", errNoSlugResolver
	}
	id, err := rt.Slugs.TeamID(v)
	if err != nil {
		return "", err
	}
	rt.Team = id
	return id, nil
}

// Project returns the resolved project id, same precedence and error contract
// as Team. A slug is looked up within the (resolved) team.
func Project(rt *config.Runtime) (string, error) {
	v := strings.TrimSpace(rt.Project)
	if v == "" {
		return "", exitcode.Usage("no project set: pass --project <id|slug>, set VIBEXP_PROJECT, or set a default on the context (vibexp config set-context %s --project <id|slug>)", contextName(rt))
	}
	if IsUUID(v) {
		return v, nil
	}
	if rt.Slugs == nil {
		return "", errNoSlugResolver
	}
	team, err := Team(rt)
	if err != nil {
		return "", err
	}
	id, err := rt.Slugs.ProjectID(team, v)
	if err != nil {
		return "", err
	}
	rt.Project = id
	return id, nil
}

// OptionalProject is Project for commands where the project only narrows the
// result: it returns "" with no error when no project is set.
func OptionalProject(rt *config.Runtime) (string, error) {
	if strings.TrimSpace(rt.Project) == "" {
		return "", nil
	}
	return Project(rt)
}

// errNoSlugResolver fails closed when a slug is resolved on a runtime that
// carries no resolver: sending the slug on would hit the server's "must be a
// valid UUID" error instead. The root installs one on every runtime it builds,
// so this means a runtime was built elsewhere — a bug, not a user error.
var errNoSlugResolver = exitcode.New(exitcode.RuntimeErr, errors.New("internal: no team/project slug resolver on the runtime"))

// IsUUID reports whether v is a canonical 36-character UUID (uuid.Validate
// alone also accepts the urn: and braced forms, which REST paths do not).
func IsUUID(v string) bool {
	return len(v) == 36 && uuid.Validate(v) == nil
}

// MissingBaseURLError is the usage error for a runtime with no base URL. It
// names both ways to set one: the environment (natural in containers, next to
// the API key) and the context.
func MissingBaseURLError(rt *config.Runtime) error {
	return exitcode.Usage("no base URL: set %s, or run: vibexp config set-context %s --base-url <url>", config.EnvBaseURL, contextName(rt))
}

func contextName(rt *config.Runtime) string {
	if rt.ContextName == "" {
		return "<name>"
	}
	return rt.ContextName
}

// teamPageSize is listTeams' maximum page_size.
const teamPageSize = 100

// NewSlugResolver returns the SlugResolver the root installs on the runtime it
// builds. It is lazy: the credential store is opened and a RawClient built only
// on the first lookup (then reused), so a command that never resolves a slug —
// or never needs credentials at all — pays nothing. rt is read at that first
// lookup, for its base URL, timeout and context. ctx is the root's command
// context, the parent of every command's own.
func NewSlugResolver(ctx context.Context, rt *config.Runtime, creds func() (*cred.Store, error), getenv func(string) string) config.SlugResolver {
	return &slugLookup{ctx: ctx, rt: rt, creds: creds, getenv: getenv}
}

// slugLookup is the SlugResolver NewSlugResolver returns.
type slugLookup struct {
	ctx    context.Context
	rt     *config.Runtime
	creds  func() (*cred.Store, error)
	getenv func(string) string

	once sync.Once
	raw  *RawClient
	err  error
}

// client builds the lookup's RawClient on first use and returns the same
// client (or error) thereafter.
func (l *slugLookup) client() (*RawClient, error) {
	l.once.Do(func() {
		store, err := l.creds()
		if err != nil {
			l.err = exitcode.New(exitcode.RuntimeErr, err)
			return
		}
		l.raw, l.err = NewRaw(l.ctx, l.rt, store, l.getenv)
	})
	return l.raw, l.err
}

// TeamID pages through the caller's teams (GET /api/v1/teams) for one whose
// slug matches.
func (l *slugLookup) TeamID(slug string) (string, error) {
	for page := 1; ; page++ {
		var body struct {
			Teams []struct {
				ID   string `json:"id"`
				Slug string `json:"slug"`
			} `json:"teams"`
			TotalCount int `json:"total_count"`
		}
		path := fmt.Sprintf("/api/v1/teams?page=%d&page_size=%d", page, teamPageSize)
		if _, err := l.getJSON(path, &body); err != nil {
			return "", err
		}
		for _, t := range body.Teams {
			if t.Slug == slug {
				return t.ID, nil
			}
		}
		if len(body.Teams) < teamPageSize || page*teamPageSize >= body.TotalCount {
			return "", exitcode.Usage("team %q not found: pass a team id or slug from `vibexp team list`", slug)
		}
	}
}

// ProjectID fetches the project by slug within the team
// (GET /api/v1/{team}/projects/{slug}).
func (l *slugLookup) ProjectID(teamID, slug string) (string, error) {
	var body struct {
		ID string `json:"id"`
	}
	status, err := l.getJSON("/api/v1/"+teamID+"/projects/"+url.PathEscape(slug), &body)
	if status == http.StatusNotFound {
		return "", exitcode.Usage("project %q not found in the team: pass a project id or slug from `vibexp project list`", slug)
	}
	if err != nil {
		return "", err
	}
	if body.ID == "" {
		return "", exitcode.New(exitcode.RuntimeErr, fmt.Errorf("project %q: response carries no id", slug))
	}
	return body.ID, nil
}

// getJSON GETs path, maps a non-2xx through Check, and decodes the body into
// out. The status is returned alongside any error so callers can special-case
// it.
func (l *slugLookup) getJSON(path string, out any) (int, error) {
	client, err := l.client()
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(l.ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return 0, err
	}
	raw, err := ReadBody(resp)
	if err != nil {
		return resp.StatusCode, exitcode.New(exitcode.RuntimeErr, err)
	}
	if cerr := Check(resp.StatusCode, raw); cerr != nil {
		return resp.StatusCode, cerr
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return resp.StatusCode, exitcode.New(exitcode.RuntimeErr, fmt.Errorf("decode %s: %w", path, err))
	}
	return resp.StatusCode, nil
}
