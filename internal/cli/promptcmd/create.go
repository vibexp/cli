package promptcmd

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
	var bodyFile, name, description, status string
	var labels []string
	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a prompt",
		Long: "Create a prompt with the given slug. The body is read from --body-file\n" +
			"(a path or '-' for stdin) and --name is required. The project is resolved\n" +
			"from --project, VIBEXP_PROJECT, or the active context.",
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
			body, err := readBodyFile(cmd, bodyFile)
			if err != nil {
				return err
			}
			if len(strings.TrimSpace(string(body))) == 0 {
				return exitcode.Usage("prompt body is required: pass --body-file <path|->")
			}
			if strings.TrimSpace(name) == "" {
				return exitcode.Usage("prompt name is required: pass --name")
			}
			base, err := basePath(rt)
			if err != nil {
				return err
			}

			payload := map[string]any{
				"project_id": project,
				"slug":       args[0],
				"name":       name,
				"body":       string(body),
			}
			if description != "" {
				payload["description"] = description
			}
			if len(labels) > 0 {
				payload["labels"] = labels
			}
			if status != "" {
				payload["status"] = status
			}
			if cmd.Flags().Changed("mcp-expose") {
				expose, _ := cmd.Flags().GetBool("mcp-expose")
				payload["mcp_expose"] = expose
			}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPost, base, payload, &itemSpec)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "file with the prompt body, or '-' for stdin")
	cmd.Flags().StringVar(&name, "name", "", "human-readable name (required)")
	cmd.Flags().StringVar(&description, "description", "", "optional description")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label for categorizing (repeatable)")
	cmd.Flags().StringVar(&status, "status", "", "initial status")
	cmd.Flags().Bool("mcp-expose", true, "expose this prompt via MCP tools")
	return cmd
}
