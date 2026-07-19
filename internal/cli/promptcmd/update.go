package promptcmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

func newUpdate(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var bodyFile, name, description, status string
	var labels []string
	cmd := &cobra.Command{
		Use:   "update <slug>",
		Short: "Update a prompt",
		Long:  "Update a prompt's body (--body-file), name, description, labels, status, MCP exposure, or project (--project). At least one must be given.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}

			payload := map[string]any{}
			body, err := readBodyFile(cmd, bodyFile)
			if err != nil {
				return err
			}
			if body != nil {
				payload["body"] = string(body)
			}
			if name != "" {
				payload["name"] = name
			}
			if description != "" {
				payload["description"] = description
			}
			if cmd.Flags().Changed("label") {
				payload["labels"] = labels
			}
			if status != "" {
				payload["status"] = status
			}
			if cmd.Flags().Changed("mcp-expose") {
				expose, _ := cmd.Flags().GetBool("mcp-expose")
				payload["mcp_expose"] = expose
			}
			// Move to a project only when --project is explicitly given (never
			// from a context/env default, which would silently move it).
			if cmd.Flags().Changed("project") {
				project, _ := cmd.Flags().GetString("project")
				payload["project_id"] = project
			}
			if len(payload) == 0 {
				return exitcode.Usage("nothing to update: pass --body-file, --name, --description, --label, --status, --mcp-expose, or --project")
			}

			itemURL, err := itemPath(rt, args[0])
			if err != nil {
				return err
			}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPut, itemURL, payload, &itemSpec)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "file with new body, or '-' for stdin")
	cmd.Flags().StringVar(&name, "name", "", "new name")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "replacement label (repeatable)")
	cmd.Flags().StringVar(&status, "status", "", "new status")
	cmd.Flags().Bool("mcp-expose", true, "expose this prompt via MCP tools")
	return cmd
}
