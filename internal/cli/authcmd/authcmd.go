// Package authcmd implements `vibexp auth` — the API-key authentication path:
// login (--with-api-key), status, and logout. Credentials live in the cred
// store; the full client factory (retry, uniform RFC 7807 mapping) arrives in
// issue #6, so login/status validate with a minimal one-off client here.
package authcmd

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	vibexp "github.com/vibexp/api-client-go"

	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/logging"
	"github.com/vibexp/cli/internal/version"
)

// StoreResolver returns the effective credential store.
type StoreResolver func() (*cred.Store, error)

// New builds the `auth` command group.
func New(resolve StoreResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate the CLI and inspect auth state",
	}
	cmd.AddCommand(
		newLogin(resolve),
		newStatus(resolve, getenv),
		newLogout(resolve),
	)
	return cmd
}

// requireRuntime returns the resolved runtime or a usage error.
func requireRuntime(ctx context.Context) (*config.Runtime, error) {
	rt := clictx.Runtime(ctx)
	if rt == nil {
		return nil, exitcode.New(exitcode.RuntimeErr, fmt.Errorf("internal: runtime not initialized"))
	}
	return rt, nil
}

// requireContextName returns the active context name or a usage error, since
// credentials are keyed by context.
func requireContextName(rt *config.Runtime) (string, error) {
	if rt.ContextName == "" {
		return "", exitcode.Usage("no active context; create one first with: vibexp config set-context <name> --base-url <url>")
	}
	return rt.ContextName, nil
}

// requireBaseURL returns the resolved base URL or a usage error.
func requireBaseURL(rt *config.Runtime) (string, error) {
	if rt.BaseURL == "" {
		return "", exitcode.Usage("no base URL for the active context; set one with: vibexp config set-context %s --base-url <url>", rt.ContextName)
	}
	return rt.BaseURL, nil
}

// fetchIdentity validates a bearer token by calling GET /api/v1/auth/me and
// returns the authenticated user. It maps failures to CLI exit codes: an
// invalid/expired credential (401) -> exit 4 with the server's RFC 7807 detail;
// anything else -> exit 1. The key is registered with the log redactor before
// the request so it can never be logged.
func fetchIdentity(ctx context.Context, baseURL, bearer string, timeout time.Duration) (*vibexp.CurrentUser, error) {
	logging.RegisterSecret(bearer)

	client, err := vibexp.NewClientWithResponses(baseURL,
		vibexp.WithHTTPClient(&http.Client{Timeout: timeout}),
		vibexp.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+bearer)
			req.Header.Set("User-Agent", "vibexp-cli/"+version.Version)
			return nil
		}),
	)
	if err != nil {
		return nil, exitcode.New(exitcode.RuntimeErr, fmt.Errorf("build client: %w", err))
	}

	resp, err := client.GetMeWithResponse(ctx)
	if err != nil {
		return nil, exitcode.New(exitcode.RuntimeErr, fmt.Errorf("contact %s: %w", baseURL, err))
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, exitcode.New(exitcode.AuthErr, problem(resp.ApplicationproblemJSON401,
			"authentication failed (invalid or expired credential)"))
	}
	return nil, exitcode.New(exitcode.RuntimeErr, problem(resp.ApplicationproblemJSON404,
		fmt.Sprintf("unexpected response from %s: %s", baseURL, resp.Status())))
}

// problem renders an RFC 7807 ErrorResponse into a concise CLI error, always
// surfacing the request id when present. Falls back to the given default.
func problem(er *vibexp.ErrorResponse, fallback string) error {
	if er == nil {
		return fmt.Errorf("%s", fallback)
	}
	msg := er.Detail
	if msg == "" {
		msg = er.Title
	}
	if msg == "" {
		msg = fallback
	}
	if er.RequestId != "" {
		return fmt.Errorf("%s (request_id: %s)", msg, er.RequestId)
	}
	return fmt.Errorf("%s", msg)
}
