package feedcmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/output"
)

// replyColumns tabulate the replies shown beneath an item in human output.
var replyColumns = []output.Column{
	{Header: "AUTHOR", Path: ".ai_assistant_name"},
	{Header: "CONTENT", Path: ".content[0:80]"},
	{Header: "POSTED", Path: ".posted_at"},
}

func newGetItem(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	return &cobra.Command{
		Use:   "get-item <item-id>",
		Short: "Show a feed item and its replies",
		Long: "Show a single feed item. In table/text output its replies are listed\n" +
			"beneath it; --format=json|yaml prints the raw item (fetch replies\n" +
			"separately via the API for a machine-readable thread).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			base, err := teamBase(rt)
			if err != nil {
				return err
			}
			itemURL := base + "/feed-items/" + args[0]
			body, _, err := resource.Do(ctx, client, http.MethodGet, itemURL, nil)
			if err != nil {
				return err
			}
			if err := resource.Render(cmd, rt, getenv, body, &itemSpec); err != nil {
				return err
			}

			// Machine output is a single raw body (pipe-safe); the threaded
			// replies view is only appended for human formats.
			if !humanFormat(rt) {
				return nil
			}
			// The item is the primary result and is already shown — a failure
			// fetching its replies is a non-fatal warning, not a command failure.
			replies, _, err := resource.Do(ctx, client, http.MethodGet, itemURL+"/replies", nil)
			if err != nil {
				cmd.PrintErrln("\nWarning: could not load replies:", err)
				return nil
			}
			cmd.PrintErrln("\nReplies:")
			return resource.Render(cmd, rt, getenv, replies,
				&output.TableSpec{Rows: ".replies[]? // .items[]? // .data[]?", Columns: replyColumns})
		},
	}
}

// humanFormat reports whether output is a human table/text view (not json/yaml
// and no --jq expression), so a secondary section can be appended safely.
func humanFormat(rt *config.Runtime) bool {
	if rt.JQ != "" {
		return false
	}
	switch output.Format(rt.Format) {
	case output.FormatJSON, output.FormatYAML:
		return false
	default:
		return true
	}
}
