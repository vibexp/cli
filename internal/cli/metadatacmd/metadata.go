// Package metadatacmd implements `vibexp metadata keys|values` — discovery of
// the metadata keys/values a team actually uses, backing the --metadata filter
// on the artifact/blueprint/memory list commands (platform v0.9.0, epic #519).
package metadatacmd

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// resourceTypes is the spec's enum for the catalog's resource_type parameter.
var resourceTypes = map[string]bool{"artifacts": true, "blueprints": true, "memories": true}

var keyColumns = []output.Column{{Header: "KEY", Path: "."}}
var valueColumns = []output.Column{{Header: "VALUE", Path: "."}}

// New builds the `metadata` command group.
func New(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metadata",
		Short: "Discover the metadata keys and values in use (backs --metadata filters)",
	}
	cmd.AddCommand(
		newCatalog(resolve, getenv, "keys", "List the metadata keys in use",
			"List the distinct metadata keys present on your rows of the given\nresource type. Use these as the key side of --metadata key=value filters.",
			output.TableSpec{Rows: ".keys[]?", Columns: keyColumns}),
		newCatalog(resolve, getenv, "values", "List the values stored under a metadata key",
			"List the distinct values stored under one metadata key on your rows of\nthe given resource type (arrays flattened, scalars as text). --q narrows\nfor typeahead.",
			output.TableSpec{Rows: ".values[]?", Columns: valueColumns}),
	)
	return cmd
}

func newCatalog(resolve resource.CredResolver, getenv config.Getenv, verb, short, long string, spec output.TableSpec) *cobra.Command {
	var resourceType, key, query string
	var limit int
	cmd := &cobra.Command{
		Use:   verb,
		Short: short,
		Long:  long + "\n\n--format=json returns the full API envelope (incl. `truncated`).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !resourceTypes[resourceType] {
				return exitcode.Usage("--type must be one of: artifacts, blueprints, memories")
			}
			if verb == "values" && key == "" {
				return exitcode.Usage("--key is required for metadata values")
			}
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			team, err := api.Team(rt)
			if err != nil {
				return err
			}
			q := url.Values{"resource_type": {resourceType}}
			if verb == "values" {
				q.Set("key", key)
			}
			if query != "" {
				q.Set("q", query)
			}
			if rt.Project != "" {
				q.Set("project_id", rt.Project)
			}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			path := "/api/v1/" + team + "/metadata/" + verb + "?" + q.Encode()
			body, err := resource.FetchJSON(ctx, client, http.MethodGet, path)
			if err != nil {
				return err
			}
			return resource.Render(cmd, rt, getenv, body, &spec)
		},
	}
	cmd.Flags().StringVar(&resourceType, "type", "", "resource type: artifacts, blueprints, or memories (required)")
	_ = cmd.MarkFlagRequired("type")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum entries to return (server default 100, max 500)")
	if verb == "values" {
		cmd.Flags().StringVar(&key, "key", "", "metadata key whose values to enumerate (required)")
		cmd.Flags().StringVar(&query, "q", "", "typeahead filter on values")
	}
	return cmd
}
