package relationcmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

// newCreate creates a typed edge between two resources. The six edge fields are
// small enum/id values (no freeform content), so they are flags rather than a
// body file. Creation is idempotent: a duplicate edge returns the existing row
// (200) instead of a new one (201) — both render the same way.
func newCreate(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var fromType, fromID, toType, toID, relationType, origin string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a typed relation between two resources",
		Long: "Create a directed, typed edge from one resource to another within a\n" +
			"project. Both endpoints must exist in the team and share a project, and\n" +
			"the object type must satisfy the relation-type matrix (governed-by →\n" +
			"blueprint, built-from → prompt, explained-by → memory, supersedes → same\n" +
			"type as the subject). Idempotent: a duplicate edge returns the existing one.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, req := range []struct{ name, val string }{
				{"--from-type", fromType},
				{"--from-id", fromID},
				{"--to-type", toType},
				{"--to-id", toID},
				{"--relation-type", relationType},
				{"--origin", origin},
			} {
				if req.val == "" {
					return exitcode.Usage("%s is required", req.name)
				}
			}

			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			base, err := basePath(rt)
			if err != nil {
				return err
			}
			payload := map[string]any{
				"from_type":     fromType,
				"from_id":       fromID,
				"to_type":       toType,
				"to_id":         toID,
				"relation_type": relationType,
				"origin":        origin,
			}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPost, base, payload, &itemSpec)
		},
	}
	cmd.Flags().StringVar(&fromType, "from-type", "", "subject resource type: artifact|memory|prompt|blueprint (required)")
	cmd.Flags().StringVar(&fromID, "from-id", "", "subject resource id (required)")
	cmd.Flags().StringVar(&toType, "to-type", "", "object resource type: artifact|memory|prompt|blueprint (required)")
	cmd.Flags().StringVar(&toID, "to-id", "", "object resource id (required)")
	cmd.Flags().StringVar(&relationType, "relation-type", "", "edge intent: governed-by|built-from|explained-by|supersedes (required)")
	cmd.Flags().StringVar(&origin, "origin", "human", "who proposed the edge: human|ai")
	return cmd
}
