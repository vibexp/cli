package relationcmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
)

// newSeed triggers a one-shot, per-team embedding-similarity backfill that
// proposes typed relations (origin=ai, status=suggested). It runs in the
// background and returns 202 immediately with no body.
func newSeed(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "Seed suggested relations from embedding similarity (background)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			base, err := basePath(rt)
			if err != nil {
				return err
			}
			if _, _, err := resource.Do(ctx, client, http.MethodPost, base+"/seed", nil); err != nil {
				return err
			}
			cmd.PrintErrln("Relation seed backfill accepted; running in the background.")
			return nil
		},
	}
}
