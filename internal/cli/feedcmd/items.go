package feedcmd

import (
	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

func newItems(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var feed string
	cmd := &cobra.Command{
		Use:   "items --feed <id>",
		Short: "List items in a feed",
		Args:  cobra.NoArgs,
	}
	page := resource.AddPaginationFlags(cmd)
	cmd.Flags().StringVar(&feed, "feed", "", "feed id (required)")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if feed == "" {
			return exitcode.Usage("--feed <id> is required (see: vibexp feed list)")
		}
		return resource.RunList(cmd, resolve, getenv, resource.ListConfig{
			PathFor: func(rt *config.Runtime) (string, error) {
				base, err := teamBase(rt)
				if err != nil {
					return "", err
				}
				return base + "/feeds/" + feed + "/items", nil
			},
			Spec: output.TableSpec{Rows: resource.ListRows("items", "feed_items"), Columns: itemColumns},
		}, page)
	}
	return cmd
}
