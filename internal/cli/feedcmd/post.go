package feedcmd

import (
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

func newPost(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var feed, title, bodyFile, author string
	cmd := &cobra.Command{
		Use:   "post [message] --feed <id> --title <title>",
		Short: "Post an item to a feed",
		Long: "Post an item to a feed. The message content comes from a positional\n" +
			"argument or --body-file (a path, or '-' for stdin). --feed and --title\n" +
			"are required.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if feed == "" {
				return exitcode.Usage("--feed <id> is required (see: vibexp feed list)")
			}
			if strings.TrimSpace(title) == "" {
				return exitcode.Usage("--title is required")
			}
			message, err := messageInput(cmd, firstArg(args), bodyFile)
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
			payload := map[string]any{
				"title":             title,
				"content":           message,
				"ai_assistant_name": author,
			}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPost,
				base+"/feeds/"+feed+"/items", payload, &itemSpec)
		},
	}
	cmd.Flags().StringVar(&feed, "feed", "", "feed id to post to (required)")
	cmd.Flags().StringVar(&title, "title", "", "item title (required)")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "file with the message, or '-' for stdin")
	cmd.Flags().StringVar(&author, "author", defaultAuthor, "AI assistant name recorded on the item")
	return cmd
}

// firstArg returns the first positional argument, or "" when none was given.
func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}
