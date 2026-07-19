package artifactcmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

func newUpdate(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var bodyFile, title, description, typ, status, changeSummary string
	cmd := &cobra.Command{
		Use:   "update <slug>",
		Short: "Update an artifact",
		Long:  "Update an artifact's content (--body-file), title, description, type, or status. At least one must be given. Resolves the project from --project, VIBEXP_PROJECT, or the active context.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}

			payload := map[string]any{}
			content, err := readBodyFile(cmd, bodyFile)
			if err != nil {
				return err
			}
			if content != nil {
				payload["content"] = string(content)
			}
			if title != "" {
				payload["title"] = title
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
			if changeSummary != "" {
				payload["change_summary"] = changeSummary
			}
			if len(payload) == 0 {
				return exitcode.Usage("nothing to update: pass --body-file, --title, --description, --type, or --status")
			}

			itemURL, err := itemPath(rt, args[0])
			if err != nil {
				return err
			}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPut, itemURL, payload, &itemSpec)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "file with new content, or '-' for stdin")
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	cmd.Flags().StringVar(&typ, "type", "", "new type category")
	cmd.Flags().StringVar(&status, "status", "", "new status")
	cmd.Flags().StringVar(&changeSummary, "change-summary", "", "summary recorded on the version snapshot")
	return cmd
}
