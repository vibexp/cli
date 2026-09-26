package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

// Team returns the resolved team id for a team-scoped command. The precedence
// (--team > VIBEXP_TEAM > active-context default) is already applied in
// config.Resolve. REST paths take only a UUID, so a slug is looked up through
// the runtime's SlugResolver (installed by NewRaw, which must therefore run
// first) and the UUID is written back
// to rt, so later calls in the same invocation make no request. A missing team
// is a usage error (exit 2) naming all three ways to set it.
func Team(rt *config.Runtime) (string, error) {
	v := strings.TrimSpace(rt.Team)
	if v == "" {
		return "", exitcode.Usage("no team set: pass --team <id|slug>, set VIBEXP_TEAM, or set a default on the context (vibexp config set-context %s --team <id|slug>)", contextName(rt))
	}
	if isUUID(v) {
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
	if isUUID(v) {
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

// errNoSlugResolver fails closed when a slug is resolved before NewRaw
// installed the resolver: sending the slug on would hit the server's
// "must be a valid UUID" error instead. It means a command built its path
// before its client — a bug, not a user error.
var errNoSlugResolver = exitcode.New(exitcode.RuntimeErr, errors.New("internal: team/project slug resolved before the API client was built"))

// isUUID reports whether v is a canonical 36-character UUID (uuid.Validate
// alone also accepts the urn: and braced forms, which REST paths do not).
func isUUID(v string) bool {
	return len(v) == 36 && uuid.Validate(v) == nil
}

func contextName(rt *config.Runtime) string {
	if rt.ContextName == "" {
		return "<name>"
	}
	return rt.ContextName
}

// teamPageSize is listTeams' maximum page_size.
const teamPageSize = 100

// slugLookup is the SlugResolver NewRaw installs: it resolves slugs over the
// same authenticated client the command itself uses.
type slugLookup struct {
	ctx    context.Context
	client *RawClient
}

// TeamID pages through the caller's teams (GET /api/v1/teams) for one whose
// slug matches.
func (l slugLookup) TeamID(slug string) (string, error) {
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
func (l slugLookup) ProjectID(teamID, slug string) (string, error) {
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
func (l slugLookup) getJSON(path string, out any) (int, error) {
	resp, err := l.client.Do(l.ctx, http.MethodGet, path, nil, nil)
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
