// Package attachmentcmd implements `vibexp attachment` — streamed multipart file
// upload plus list and delete. The generated client exposes only raw
// `...WithBody` upload variants, so the CLI builds the multipart body itself via
// api.Stream (io.Pipe + multipart.Writer, bounded memory). Attachments are
// polymorphically owned, so `upload`/`list` take --owner-id (required) and
// --owner-type (default "artifact").
package attachmentcmd

import (
	"net/url"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// defaultOwnerType is the only owner type the server currently supports.
const defaultOwnerType = "artifact"

var columns = []output.Column{
	{Header: "ID", Path: ".id"},
	{Header: "FILENAME", Path: ".file_name"},
	{Header: "SIZE", Path: ".size_bytes"},
	{Header: "CONTENT-TYPE", Path: ".content_type"},
	{Header: "CREATED", Path: ".created_at"},
}

var itemSpec = output.TableSpec{Rows: ".", Columns: columns}

// New builds the `attachment` command group.
func New(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attachment",
		Short: "Upload, list, and delete attachments",
	}
	cmd.AddCommand(
		newUpload(resolve, getenv),
		newList(resolve, getenv),
		newDelete(resolve, getenv),
	)
	return cmd
}

// basePath returns /api/v1/{team}/attachments, resolving the team (missing →
// exit 2).
func basePath(rt *config.Runtime) (string, error) {
	team, err := api.Team(rt)
	if err != nil {
		return "", err
	}
	return "/api/v1/" + team + "/attachments", nil
}

func newList(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var ownerID, ownerType string
	cmd := &cobra.Command{
		Use:   "list --owner-id <id>",
		Short: "List attachments for an owner",
		Long:  "List the attachments owned by a resource. --owner-id is required; --owner-type defaults to \"artifact\".",
		Args:  cobra.NoArgs,
	}
	page := resource.AddPaginationFlags(cmd)
	cmd.Flags().StringVar(&ownerID, "owner-id", "", "UUID of the owning resource (required)")
	cmd.Flags().StringVar(&ownerType, "owner-type", defaultOwnerType, "type of the owning resource")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if ownerID == "" {
			return exitcode.Usage("--owner-id <id> is required")
		}
		return resource.RunList(cmd, resolve, getenv, resource.ListConfig{
			PathFor: func(rt *config.Runtime) (string, error) {
				base, err := basePath(rt)
				if err != nil {
					return "", err
				}
				q := url.Values{}
				q.Set("owner_type", ownerType)
				q.Set("owner_id", ownerID)
				return base + "?" + q.Encode(), nil
			},
			Spec: output.TableSpec{Rows: ".attachments[]? // .items[]? // .data[]?", Columns: columns},
		}, page)
	}
	return cmd
}
