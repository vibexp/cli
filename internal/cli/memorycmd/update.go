package memorycmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

func newUpdate(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var bodyFile, status string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a memory",
		Long:  "Update a memory's content (--body-file), status, or project (--project). At least one must be given.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}

			payload := map[string]any{}
			text, err := readBodyFile(cmd, bodyFile)
			if err != nil {
				return err
			}
			if text != nil {
				payload["text"] = string(text)
			}
			if status != "" {
				payload["status"] = status
			}
			// Move to a project only when --project is explicitly given (never
			// from a context/env default, which would silently move it).
			if cmd.Flags().Changed("project") {
				project, _ := cmd.Flags().GetString("project")
				payload["project_id"] = project
			}
			if len(payload) == 0 {
				return exitcode.Usage("nothing to update: pass --body-file, --status, or --project")
			}

			base, err := basePath(rt)
			if err != nil {
				return err
			}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPut, base+"/"+args[0], payload, &itemSpec)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "file with new content, or '-' for stdin")
	cmd.Flags().StringVar(&status, "status", "", "new status")
	return cmd
}
