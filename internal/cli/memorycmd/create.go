package memorycmd

import (
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

func newCreate(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var bodyFile, status string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a memory",
		Long: "Create a memory. The content is read from --body-file (a path or '-'\n" +
			"for stdin). The project is resolved from --project, VIBEXP_PROJECT, or\n" +
			"the active context.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			project, err := api.Project(rt) // required; missing → exit 2
			if err != nil {
				return err
			}
			text, err := readBodyFile(cmd, bodyFile)
			if err != nil {
				return err
			}
			if len(strings.TrimSpace(string(text))) == 0 {
				return exitcode.Usage("memory content is required: pass --body-file <path|->")
			}
			base, err := basePath(rt)
			if err != nil {
				return err
			}

			payload := map[string]any{"project_id": project, "text": string(text)}
			if status != "" {
				payload["status"] = status
			}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPost, base, payload, &itemSpec)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "file with the memory content, or '-' for stdin")
	cmd.Flags().StringVar(&status, "status", "", "initial status (e.g. active)")
	return cmd
}
