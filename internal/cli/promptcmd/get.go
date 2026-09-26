package promptcmd

import (
	"encoding/json"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

func newGet(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var bodyOnly bool
	cmd := &cobra.Command{
		Use:   "get <slug>",
		Short: "Show a single prompt",
		Long: "Show a single prompt's metadata. Pass --body to print only the stored\n" +
			"prompt body, raw and byte-for-byte (unexpanded @references and {{vars}},\n" +
			"no decoration), so `prompt get <slug> --body > f` round-trips through\n" +
			"`prompt update <slug> --body-file f`. --body cannot be combined with\n" +
			"--format or --jq.",
		Args: cobra.ExactArgs(1),
	}
	showRelations := resource.AddRelationsFlag(cmd)
	cmd.Flags().BoolVar(&bodyOnly, "body", false, "print only the prompt body, raw (pipe-safe)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if bodyOnly {
			// Only an explicit flag conflicts; a VIBEXP_FORMAT default is simply
			// overridden by the more specific --body.
			for _, name := range []string{"format", "jq"} {
				if cmd.Flags().Changed(name) {
					return exitcode.Usage("--body cannot be combined with --%s", name)
				}
			}
		}
		ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
		if err != nil {
			return err
		}
		path, err := itemPath(rt, args[0])
		if err != nil {
			return err
		}
		body, _, err := resource.Do(ctx, client, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		if bodyOnly {
			var p struct {
				Body string `json:"body"`
			}
			if err := json.Unmarshal(body, &p); err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			if _, err := cmd.OutOrStdout().Write([]byte(p.Body)); err != nil {
				return err
			}
			if *showRelations {
				resource.RenderRelationsSummary(cmd, body)
			}
			return nil
		}
		return resource.RenderWithRelations(cmd, rt, getenv, body, &itemSpec, *showRelations)
	}
	return cmd
}
