// Package apicmd implements `vibexp api` — an authenticated raw passthrough to
// any API endpoint (the gh-api-style escape hatch). It reuses the shared
// transport/auth (internal/api) and the output engine (internal/output) so its
// behavior — auth, retries, exit codes, RFC 7807 rendering — matches curated
// commands.
package apicmd

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"

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

// allowedMethods are the HTTP methods `vibexp api` accepts.
var allowedMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodHead: true,
}

// New builds the `api` command.
func New(resolve CredResolver, getenv config.Getenv) *cobra.Command {
	var (
		input    string
		headers  []string
		paginate bool
	)
	cmd := &cobra.Command{
		Use:   "api <METHOD> <path>",
		Short: "Make an authenticated request to any API endpoint",
		Long: "Send an authenticated request to any endpoint and pipe the response\n" +
			"through the output engine (--format, --jq).\n\n" +
			"The path is server-relative and may contain a {team} placeholder\n" +
			"(resolved like curated commands) and an inline query string. Provide a\n" +
			"body with --input <file> or --input - (stdin).",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			method := strings.ToUpper(args[0])
			path := args[1]
			if !allowedMethods[method] {
				return exitcode.Usage("unsupported method %q: use GET, POST, PUT, PATCH, DELETE, or HEAD", args[0])
			}

			ctx := cmd.Context()
			rt := clictx.Runtime(ctx)
			if rt == nil {
				return exitcode.New(exitcode.RuntimeErr, errRuntimeMissing)
			}

			resolvedPath, err := substituteTeam(path, rt)
			if err != nil {
				return err
			}

			hdr, err := parseHeaders(headers)
			if err != nil {
				return err
			}
			body, err := readInput(cmd, input)
			if err != nil {
				return err
			}

			store, err := resolve()
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			client, err := api.NewRaw(ctx, rt, store, getenv)
			if err != nil {
				return err
			}

			if paginate {
				return runPaginate(ctx, cmd, client, method, resolvedPath, hdr, rt, getenv)
			}
			return runSingle(ctx, cmd, client, method, resolvedPath, body, hdr, rt, getenv)
		},
	}
	cmd.Flags().StringVarP(&input, "input", "i", "", "request body from a file, or '-' for stdin")
	cmd.Flags().StringArrayVarP(&headers, "header", "H", nil, "request header 'Key: Value' (repeatable, overrides defaults)")
	cmd.Flags().BoolVar(&paginate, "paginate", false, "merge all pages of a paged GET list endpoint")
	return cmd
}

// runSingle executes one request and renders the response.
func runSingle(ctx context.Context, cmd *cobra.Command, client *api.RawClient, method, path string, body []byte, hdr http.Header, rt *config.Runtime, getenv config.Getenv) error {
	resp, err := client.Do(ctx, method, path, body, hdr)
	if err != nil {
		return err
	}
	raw, err := api.ReadBody(resp)
	if err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}
	if cerr := api.Check(resp.StatusCode, raw); cerr != nil {
		return cerr
	}
	return renderBody(cmd, rt, getenv, raw)
}

// renderBody pipes a response body through the output engine to stdout.
func renderBody(cmd *cobra.Command, rt *config.Runtime, getenv config.Getenv, raw []byte) error {
	opts := output.Options{
		Format: output.Format(rt.Format),
		IsTTY:  rt.IsTTY,
		Color:  output.ColorEnabled(rt.IsTTY, getenv),
		JQ:     rt.JQ,
	}
	return output.Render(cmd.OutOrStdout(), raw, nil, opts)
}

// substituteTeam replaces a {team} placeholder using the shared resolver.
func substituteTeam(path string, rt *config.Runtime) (string, error) {
	if !strings.Contains(path, "{team}") {
		return path, nil
	}
	team, err := api.Team(rt)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(path, "{team}", team), nil
}

// parseHeaders turns repeated "Key: Value" flags into an http.Header.
func parseHeaders(entries []string) (http.Header, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	h := http.Header{}
	for _, e := range entries {
		k, v, ok := strings.Cut(e, ":")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, exitcode.Usage("invalid --header %q: want 'Key: Value'", e)
		}
		h.Add(strings.TrimSpace(k), strings.TrimSpace(v))
	}
	return h, nil
}

// readInput reads the request body from a file or stdin ('-'); empty means no
// body.
func readInput(cmd *cobra.Command, input string) ([]byte, error) {
	switch input {
	case "":
		return nil, nil
	case "-":
		body, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, exitcode.New(exitcode.RuntimeErr, err)
		}
		return body, nil
	default:
		body, err := os.ReadFile(input)
		if err != nil {
			return nil, exitcode.Usage("read --input %q: %v", input, err)
		}
		return body, nil
	}
}
