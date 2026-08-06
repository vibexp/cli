// Package cli assembles the cobra command tree for the vibexp CLI: the root
// command, its global flags, and the persistent pre-run that initializes
// logging and resolves the active context into the command context.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/apicmd"
	"github.com/vibexp/cli/internal/cli/artifactcmd"
	"github.com/vibexp/cli/internal/cli/attachmentcmd"
	"github.com/vibexp/cli/internal/cli/authcmd"
	"github.com/vibexp/cli/internal/cli/blueprintcmd"
	"github.com/vibexp/cli/internal/cli/configcmd"
	"github.com/vibexp/cli/internal/cli/feedcmd"
	"github.com/vibexp/cli/internal/cli/memorycmd"
	"github.com/vibexp/cli/internal/cli/metadatacmd"
	"github.com/vibexp/cli/internal/cli/projectcmd"
	"github.com/vibexp/cli/internal/cli/promptcmd"
	"github.com/vibexp/cli/internal/cli/relationcmd"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/cli/searchcmd"
	"github.com/vibexp/cli/internal/cli/teamcmd"
	"github.com/vibexp/cli/internal/cli/updatecmd"
	"github.com/vibexp/cli/internal/cli/usercmd"
	"github.com/vibexp/cli/internal/cli/versioncmd"
	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/logging"
	"github.com/vibexp/cli/internal/output"
	"github.com/vibexp/cli/internal/update"
)

// defaultTimeout is the fallback request timeout when --timeout is unset.
const defaultTimeout = 30 * time.Second

// globalFlags holds the values bound to the root command's persistent flags.
type globalFlags struct {
	context string
	team    string
	project string
	format  string
	jq      string
	debug   bool
	timeout time.Duration
}

// Options tweak command-tree construction (primarily for tests).
type Options struct {
	// Store overrides the config store (defaults to ~/.vibexp/config.yaml).
	Store *config.Store
	// CredStore overrides the credential store (defaults to
	// ~/.vibexp/credentials.json).
	CredStore *cred.Store
	// Getenv overrides environment lookups (defaults to os.Getenv).
	Getenv config.Getenv
	// LogDir overrides the log directory (defaults to ~/.vibexp/logs).
	LogDir string
}

// rootState captures per-invocation state that Execute needs after the command
// tree has run (chiefly the logger, so it can be flushed/closed).
type rootState struct {
	logger *logging.Logger
}

// NewRootCommand builds the full command tree.
func NewRootCommand(opts Options) *cobra.Command {
	cmd, _ := newRoot(opts)
	return cmd
}

// newRoot builds the command tree and returns the shared rootState so callers
// can close the logger after execution.
func newRoot(opts Options) (*cobra.Command, *rootState) {
	st := &rootState{}
	gf := &globalFlags{}
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	root := &cobra.Command{
		Use:           "vibexp",
		Short:         "VibeXP command-line interface",
		Long:          "vibexp is the command-line interface for the VibeXP platform.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Persistent pre-run initializes logging and resolves the runtime for
		// every command.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := logging.Init(logging.Options{Dir: opts.LogDir, Debug: gf.debug})
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			st.logger = logger

			store := opts.Store
			if store == nil {
				store, err = config.DefaultStore()
				if err != nil {
					return exitcode.New(exitcode.RuntimeErr, err)
				}
			}
			file, err := store.Load()
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}

			rt := file.Resolve(config.Overrides{
				Context: gf.context,
				Team:    gf.team,
				Project: gf.project,
				Format:  gf.format,
				Timeout: gf.timeout,
			}, getenv)

			// Validate the resolved format and populate output-only fields.
			if _, err := output.ParseFormat(rt.Format); err != nil {
				return err
			}
			rt.JQ = gf.jq
			rt.IsTTY = output.IsTerminal(os.Stdout)

			ctx := clictx.WithLogger(cmd.Context(), logger)
			ctx = clictx.WithRuntime(ctx, &rt)
			cmd.SetContext(ctx)

			// Always-on: one info line per invocation so the log is populated
			// even without --debug (rotation/redaction are exercised in real use).
			logger.Info("command invoked",
				"command", cmd.CommandPath(),
				"context", rt.ContextName,
			)
			return nil
		},
		// Root with no subcommand prints help and exits 0.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	// Flag parse errors are usage errors (exit 2).
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitcode.New(exitcode.UsageErr, err)
	})

	pf := root.PersistentFlags()
	pf.StringVar(&gf.context, "context", "", "config context to use (overrides active context)")
	pf.StringVar(&gf.team, "team", "", "team id or slug (overrides context default)")
	pf.StringVar(&gf.project, "project", "", "project id or slug (overrides context default)")
	pf.StringVar(&gf.format, "format", "", "output format: json|yaml|table|text (default: table on a terminal, TSV when piped)")
	pf.StringVar(&gf.jq, "jq", "", "filter JSON output with a gojq expression")
	pf.BoolVar(&gf.debug, "debug", false, "mirror debug logs to stderr")
	pf.DurationVar(&gf.timeout, "timeout", defaultTimeout, "per-request timeout")

	// storeResolver hands config subcommands the effective store (test override
	// or the default ~/.vibexp/config.yaml).
	storeResolver := func() (*config.Store, error) {
		if opts.Store != nil {
			return opts.Store, nil
		}
		return config.DefaultStore()
	}
	// credResolver hands auth subcommands the effective credential store.
	credResolver := func() (*cred.Store, error) {
		if opts.CredStore != nil {
			return opts.CredStore, nil
		}
		return cred.DefaultStore()
	}

	root.AddCommand(configcmd.New(storeResolver, getenv))
	root.AddCommand(authcmd.New(credResolver, getenv))
	root.AddCommand(apicmd.New(credResolver, getenv))
	root.AddCommand(usercmd.NewWhoami(resource.CredResolver(credResolver), getenv))
	root.AddCommand(teamcmd.New(resource.CredResolver(credResolver), getenv))
	root.AddCommand(projectcmd.New(resource.CredResolver(credResolver), getenv))
	root.AddCommand(memorycmd.New(resource.CredResolver(credResolver), getenv))
	root.AddCommand(metadatacmd.New(resource.CredResolver(credResolver), getenv))
	root.AddCommand(blueprintcmd.New(resource.CredResolver(credResolver), getenv))
	root.AddCommand(promptcmd.New(resource.CredResolver(credResolver), getenv))
	root.AddCommand(artifactcmd.New(resource.CredResolver(credResolver), getenv))
	root.AddCommand(feedcmd.New(resource.CredResolver(credResolver), getenv))
	root.AddCommand(searchcmd.New(resource.CredResolver(credResolver), getenv))
	root.AddCommand(attachmentcmd.New(resource.CredResolver(credResolver), getenv))
	root.AddCommand(relationcmd.New(resource.CredResolver(credResolver), getenv))
	root.AddCommand(updatecmd.New())
	root.AddCommand(versioncmd.New())

	return root, st
}

// Execute builds and runs the root command, returning the process exit code.
// The logger is closed before returning.
func Execute(ctx context.Context, args []string) int {
	root, st := newRoot(Options{})
	root.SetArgs(args)

	err := root.ExecuteContext(ctx)

	if st.logger != nil {
		if err != nil {
			logFailure(st.logger, err)
		}
		_ = st.logger.Close()
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
	}

	// After the command has produced its output, run the cached, non-blocking
	// version check and print at most one upgrade notice to stderr. Skipped for
	// `vibexp update` itself (it already reports version state). Suppressed for
	// dev builds, CI, and VIBEXP_NO_UPDATE_CHECK via internal/update.
	if len(args) == 0 || args[0] != "update" {
		update.Notify(ctx, os.Stderr, os.Getenv)
	}

	return exitcode.FromError(err)
}

// logFailure records a command failure to the file log, surfacing the API
// request id when the error carries one (RFC 7807).
func logFailure(logger *logging.Logger, err error) {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		logger.Error("command failed",
			"error", err.Error(),
			"status", apiErr.Status,
			"request_id", apiErr.RequestID,
		)
		return
	}
	logger.Error("command failed", "error", err.Error())
}
