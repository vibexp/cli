package blueprintcmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

func newUpdate(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var bodyFile, title, description, typ, subtype, path, status string
	cmd := &cobra.Command{
		Use:   "update <slug>",
		Short: "Update a blueprint",
		Long:  "Update a blueprint's content (--body-file), title, description, type, subtype, path, or status. At least one must be given. Resolves the project from --project, VIBEXP_PROJECT, or the active context.",
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
			if subtype != "" {
				payload["subtype"] = subtype
			}
			if path != "" {
				payload["path"] = path
			}
			if status != "" {
				payload["status"] = status
			}
			if len(payload) == 0 {
				return exitcode.Usage("nothing to update: pass --body-file, --title, --description, --type, --subtype, --path, or --status")
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
	cmd.Flags().StringVar(&subtype, "subtype", "", "new subtype category")
	cmd.Flags().StringVar(&path, "path", "", "new repo-relative path")
	cmd.Flags().StringVar(&status, "status", "", "new status")
	return cmd
}
