package memorycmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

func newUpdate(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var bodyFile, status, title string
	var meta resource.MetadataFlags
	var labels resource.LabelFlags
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a memory",
		Long: "Update a memory's content (--body-file), title, status, project\n" +
			"(--project), labels, or metadata. At least one must be given. --title \"\"\n" +
			"clears the title.\n\n" + resource.MetadataUpdateHelp,
		Args: cobra.ExactArgs(1),
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
			// Send a title only when --title is given, so an update never clears
			// it implicitly; --title "" sends null, which clears it.
			if cmd.Flags().Changed("title") {
				if err := checkTitle(title); err != nil {
					return err
				}
				payload["title"] = nil
				if title != "" {
					payload["title"] = title
				}
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
			labels.AddTo(payload)
			if len(payload) == 0 && !meta.Set() {
				return exitcode.Usage("nothing to update: pass --body-file, --title, --status, --project, --label, or a metadata flag")
			}

			base, err := basePath(rt)
			if err != nil {
				return err
			}
			itemURL := base + "/" + args[0]
			if err := meta.AddMerged(ctx, client, itemURL, payload); err != nil {
				return err
			}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPut, itemURL, payload, &itemSpec)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "file with new content, or '-' for stdin")
	cmd.Flags().StringVar(&title, "title", "", titleUsage+`; "" clears it`)
	cmd.Flags().StringVar(&status, "status", "", "new status")
	resource.AddLabelFlags(cmd, &labels, true)
	resource.AddMetadataFlags(cmd, &meta, true)
	return cmd
}
