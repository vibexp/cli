package apicmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

var errRuntimeMissing = errors.New("internal: runtime not initialized")

// runPaginate walks a page/offset-based list endpoint (resource.FetchAllPages)
// and renders the union of all items as one JSON array. If the first page has
// no recognizable list field, it renders that page raw and stops (documented
// fallback).
func runPaginate(ctx context.Context, cmd *cobra.Command, client *api.RawClient, method, path string, hdr http.Header, rt *config.Runtime, getenv config.Getenv) error {
	if method != http.MethodGet {
		return exitcode.Usage("--paginate only supports GET requests")
	}
	items, notList, err := resource.FetchAllPages(ctx, client, path, hdr)
	if err != nil {
		return err
	}
	if notList != nil {
		return renderBody(cmd, rt, getenv, notList)
	}
	merged, err := json.Marshal(items)
	if err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}
	return renderBody(cmd, rt, getenv, merged)
}
