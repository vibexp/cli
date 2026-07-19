package feedcmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/output"
)

// replySpec renders a single created reply object.
var replySpec = output.TableSpec{Rows: ".", Columns: replyColumns}

func newReply(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var bodyFile, author string
	cmd := &cobra.Command{
		Use:   "reply <item-id> [message]",
		Short: "Reply to a feed item",
		Long: "Reply to a feed item. The reply content comes from a positional\n" +
			"argument or --body-file (a path, or '-' for stdin).",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var msgArg string
			if len(args) > 1 {
				msgArg = args[1]
			}
			message, err := messageInput(cmd, msgArg, bodyFile)
			if err != nil {
				return err
			}

			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			base, err := teamBase(rt)
			if err != nil {
				return err
			}
			payload := map[string]any{"content": message}
			if author != "" {
				payload["ai_assistant_name"] = author
			}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPost,
				base+"/feed-items/"+args[0]+"/replies", payload, &replySpec)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "file with the reply, or '-' for stdin")
	cmd.Flags().StringVar(&author, "author", defaultAuthor, "AI assistant name recorded on the reply")
	return cmd
}
