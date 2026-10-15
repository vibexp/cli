package searchcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// summaryTimeout is the request timeout --summary uses when --timeout is not
// given: the platform waits up to its configured provider timeout (60s by
// default) for the model, so the CLI's 30s default would give up first.
const summaryTimeout = 90 * time.Second

// summaryHints prepend an actionable line to the platform's documented AI
// Summary failures. Unknown codes keep the server's own detail.
var summaryHints = map[string]string{
	"AI_SUMMARY_DISABLED":             "AI summaries are disabled for this team; a team admin can enable them in team settings",
	"AI_SUMMARY_NO_PROVIDER":          "no model provider is configured for this team's AI summaries; a team admin can choose one in team settings",
	"AI_SUMMARY_NO_RESULTS":           "the search found nothing to summarize; try a broader query, or drop --type/--project",
	"AI_SUMMARY_PROVIDER_UNREACHABLE": "the team's model provider could not be reached; try again later, or ask a team admin to check the provider",
	"AI_SUMMARY_UNAUTHORIZED":         "the team's model provider rejected its credentials; a team admin should check the provider's API key",
	"AI_SUMMARY_MODEL_ERROR":          "the model failed to produce a summary; try again, or rephrase the query",
	"AI_SUMMARY_TIMEOUT":              "the model provider timed out; try again, or narrow the query with --type/--project",
}

// summarySource is one cited document of a SearchSummaryResponse.
type summarySource struct {
	Index     int    `json:"index"`
	ID        string `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	Truncated bool   `json:"truncated"`
}

// runSummary posts the search body minus paging to /search/summary and prints
// the answer followed by its numbered sources.
func runSummary(cmd *cobra.Command, resolve resource.CredResolver, getenv config.Getenv, query string, types []string) error {
	for _, name := range []string{"limit", "page"} {
		if cmd.Flags().Changed(name) {
			return exitcode.Usage("--%s cannot be used with --summary: the summary always reads the top results", name)
		}
	}
	ctx := cmd.Context()
	shared, err := resource.Runtime(ctx)
	if err != nil {
		return err
	}
	rt := *shared
	if !cmd.Flags().Changed("timeout") && rt.Timeout < summaryTimeout {
		rt.Timeout = summaryTimeout
	}
	client, err := resource.Client(ctx, &rt, resolve, getenv)
	if err != nil {
		return err
	}
	team, err := api.Team(&rt)
	if err != nil {
		return err
	}
	payload, err := buildPayload(&rt, query, types)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}
	raw, _, err := resource.Do(ctx, client, http.MethodPost, "/api/v1/"+team+"/search/summary", body)
	if err != nil {
		return summaryError(err)
	}
	if rt.JQ != "" || rt.Format == string(output.FormatJSON) || rt.Format == string(output.FormatYAML) {
		return resource.Render(cmd, &rt, getenv, raw, nil)
	}
	return writeSummary(cmd, raw)
}

// writeSummary prints the markdown answer byte-for-byte (pipe-safe, like
// `prompt render`), then a blank line and one `[n] type  title  slug` line per
// source in citation order. Memories have no slug, so they show their id.
func writeSummary(cmd *cobra.Command, raw []byte) error {
	var env struct {
		Summary string          `json:"summary"`
		Sources []summarySource `json:"sources"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}
	sort.SliceStable(env.Sources, func(i, j int) bool { return env.Sources[i].Index < env.Sources[j].Index })

	var b strings.Builder
	b.WriteString(env.Summary)
	if !strings.HasSuffix(env.Summary, "\n") {
		b.WriteString("\n")
	}
	if len(env.Sources) > 0 {
		b.WriteString("\n")
	}
	for _, s := range env.Sources {
		ref := s.Slug
		if ref == "" {
			ref = s.ID
		}
		fmt.Fprintf(&b, "[%d] %s  %s  %s", s.Index, s.Type, s.Title, ref)
		if s.Truncated {
			b.WriteString(" (truncated)")
		}
		b.WriteString("\n")
	}
	_, err := cmd.OutOrStdout().Write([]byte(b.String()))
	return err
}

// summaryError keeps a 400 a usage error (exit 2), like `search`, and puts an
// actionable line in front of a documented AI Summary failure. The server's
// detail and request_id follow it, and the exit code stays the API error's.
func summaryError(err error) error {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	if hint, ok := summaryHints[apiErr.Code]; ok {
		return exitcode.New(apiErr.ExitCode(), fmt.Errorf("%s\n%w", hint, err))
	}
	return resource.UsageOnBadRequest(err)
}
