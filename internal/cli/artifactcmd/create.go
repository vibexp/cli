package artifactcmd

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
	var bodyFile, title, description, typ, status string
	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create an artifact",
		Long: "Create an artifact with the given slug. The content is read from\n" +
			"--body-file (a path or '-' for stdin) and --title is required. The project\n" +
			"is resolved from --project, VIBEXP_PROJECT, or the active context.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			project, err := api.Project(rt) // required; missing → exit 2
			if err != nil {
				return err
			}
			content, err := readBodyFile(cmd, bodyFile)
			if err != nil {
				return err
			}
			if len(strings.TrimSpace(string(content))) == 0 {
				return exitcode.Usage("artifact content is required: pass --body-file <path|->")
			}
			if strings.TrimSpace(title) == "" {
				return exitcode.Usage("artifact title is required: pass --title")
			}
			base, err := basePath(rt)
			if err != nil {
				return err
			}

			payload := map[string]any{
				"project_id": project,
				"slug":       args[0],
				"title":      title,
				"content":    string(content),
			}
			if description != "" {
				payload["description"] = description
			}
			if typ != "" {
				payload["type"] = typ
			}
			if status != "" {
				payload["status"] = status
			}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPost, base, payload, &itemSpec)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "file with the artifact content, or '-' for stdin")
	cmd.Flags().StringVar(&title, "title", "", "human-readable title (required)")
	cmd.Flags().StringVar(&description, "description", "", "optional description")
	cmd.Flags().StringVar(&typ, "type", "", "type category (defaults to general server-side)")
	cmd.Flags().StringVar(&status, "status", "", "initial status")
	return cmd
}
