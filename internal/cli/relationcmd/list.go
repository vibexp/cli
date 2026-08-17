package relationcmd

import (
	"net/url"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/output"
)

// newList lists the relations touching a resource, in both directions. The
// resource is identified by two positional args (its type and id), matching the
// required resource_type/resource_id query params.
func newList(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <resource_type> <resource_id>",
		Short: "List a resource's relations (both directions), newest first",
		Long: "List the typed relations touching a resource, in both directions.\n" +
			"resource_type is one of artifact|memory|prompt|blueprint; resource_id is\n" +
			"the resource's identifier.",
		Args: cobra.ExactArgs(2),
	}
	page := resource.AddPaginationFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		resType, resID := args[0], args[1]
		cfg := resource.ListConfig{
			PathFor: func(rt *config.Runtime) (string, error) {
				base, err := basePath(rt)
				if err != nil {
					return "", err
				}
				q := url.Values{}
				q.Set("resource_type", resType)
				q.Set("resource_id", resID)
				return base + "?" + q.Encode(), nil
			},
			Spec: output.TableSpec{Rows: resource.ListRows("relations"), Columns: listColumns},
		}
		return resource.RunList(cmd, resolve, getenv, cfg, page)
	}
	return cmd
}
