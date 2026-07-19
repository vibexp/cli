// Package resource holds the shared scaffolding every read/list command is
// built from: obtaining an authenticated client, fetching + error-mapping a raw
// JSON response, and rendering it through the output engine. A new resource
// command needs only its endpoint path and a column spec — see
// docs/adding-commands.md.
package resource

import (
	"context"
	"errors"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// CredResolver returns the effective credential store.
type CredResolver func() (*cred.Store, error)

// Runtime extracts the resolved runtime from the command context.
func Runtime(ctx context.Context) (*config.Runtime, error) {
	rt := clictx.Runtime(ctx)
	if rt == nil {
		return nil, exitcode.New(exitcode.RuntimeErr, errors.New("internal: runtime not initialized"))
	}
	return rt, nil
}

// Client builds an authenticated raw client for the runtime.
func Client(ctx context.Context, rt *config.Runtime, resolve CredResolver, getenv config.Getenv) (*api.RawClient, error) {
	store, err := resolve()
	if err != nil {
		return nil, exitcode.New(exitcode.RuntimeErr, err)
	}
	return api.NewRaw(ctx, rt, store, getenv)
}

// FetchJSON performs a request and returns the raw response body, mapping any
// non-2xx into an *api.Error (RFC 7807 detail + request_id + exit code).
func FetchJSON(ctx context.Context, client *api.RawClient, method, path string) ([]byte, error) {
	resp, err := client.Do(ctx, method, path, nil, nil)
	if err != nil {
		return nil, err
	}
	body, err := api.ReadBody(resp)
	if err != nil {
		return nil, exitcode.New(exitcode.RuntimeErr, err)
	}
	if cerr := api.Check(resp.StatusCode, body); cerr != nil {
		return nil, cerr
	}
	return body, nil
}

// Render writes a raw response body to stdout through the output engine, using
// the runtime's format/jq/tty settings and the given column spec (nil for
// pure passthrough).
func Render(cmd *cobra.Command, rt *config.Runtime, getenv config.Getenv, body []byte, spec *output.TableSpec) error {
	opts := output.Options{
		Format: output.Format(rt.Format),
		IsTTY:  rt.IsTTY,
		Color:  output.ColorEnabled(rt.IsTTY, getenv),
		JQ:     rt.JQ,
	}
	return output.Render(cmd.OutOrStdout(), body, spec, opts)
}

// GetItem is the one-line helper a single-object command (e.g. whoami) uses:
// fetch a path and render it with a spec.
func GetItem(cmd *cobra.Command, resolve CredResolver, getenv config.Getenv, path string, spec output.TableSpec) error {
	ctx := cmd.Context()
	rt, err := Runtime(ctx)
	if err != nil {
		return err
	}
	client, err := Client(ctx, rt, resolve, getenv)
	if err != nil {
		return err
	}
	body, err := FetchJSON(ctx, client, http.MethodGet, path)
	if err != nil {
		return err
	}
	return Render(cmd, rt, getenv, body, &spec)
}
