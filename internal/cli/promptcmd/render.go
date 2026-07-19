package promptcmd

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// renderSpec drives table/text rendering of the render envelope; JSON/YAML pass
// the raw response through untouched.
var renderSpec = output.TableSpec{
	Rows:    ".",
	Columns: []output.Column{{Header: "RENDERED", Path: ".rendered_body"}},
}

func newRender(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var vars []string
	cmd := &cobra.Command{
		Use:   "render <slug>",
		Short: "Render a prompt template with variables",
		Long: "Render a prompt template, substituting repeatable --var key=value pairs.\n" +
			"By default the rendered text is printed raw to stdout (pipe-safe, no\n" +
			"decoration); pass --format=json|yaml to get the full API response\n" +
			"(rendered_body, placeholders_missing, warnings). A missing required\n" +
			"variable surfaces the API's validation error (exit 1). On a duplicate\n" +
			"--var key the last value wins.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			placeholders, err := parseVars(vars)
			if err != nil {
				return err
			}
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			itemURL, err := itemPath(rt, args[0])
			if err != nil {
				return err
			}

			payload, err := json.Marshal(map[string]any{"placeholders": placeholders})
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			raw, _, err := resource.Do(ctx, client, http.MethodPost, itemURL+"/render", payload)
			if err != nil {
				return err
			}

			// A requested format (--format / VIBEXP_FORMAT, both folded into
			// rt.Format) or a --jq expression means "give me the API response";
			// otherwise print only the rendered body, byte-for-byte (pipe-safe).
			if rt.Format != "" || rt.JQ != "" {
				return resource.Render(cmd, rt, getenv, raw, &renderSpec)
			}
			var env struct {
				RenderedBody string `json:"rendered_body"`
			}
			if err := json.Unmarshal(raw, &env); err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			_, err = cmd.OutOrStdout().Write([]byte(env.RenderedBody))
			return err
		},
	}
	cmd.Flags().StringArrayVar(&vars, "var", nil, "template variable as key=value (repeatable)")
	return cmd
}

// parseVars turns repeated key=value flags into a placeholders map, splitting on
// the first '='. A duplicate key keeps the last value.
func parseVars(vars []string) (map[string]string, error) {
	out := make(map[string]string, len(vars))
	for _, v := range vars {
		key, val, ok := strings.Cut(v, "=")
		if !ok {
			return nil, exitcode.Usage("invalid --var %q: expected key=value", v)
		}
		if key == "" {
			return nil, exitcode.Usage("invalid --var %q: empty key", v)
		}
		out[key] = val
	}
	return out, nil
}
