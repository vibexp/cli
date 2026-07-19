// Package configcmd implements `vibexp config` context-management subcommands:
// set-context, use-context, get-contexts, current-context.
package configcmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

// StoreResolver returns the effective config store.
type StoreResolver func() (*config.Store, error)

// New builds the `config` command group.
func New(resolve StoreResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration and contexts",
	}
	cmd.AddCommand(
		newSetContext(resolve),
		newUseContext(resolve),
		newGetContexts(resolve),
		newCurrentContext(resolve, getenv),
	)
	return cmd
}

func loadStore(resolve StoreResolver) (*config.Store, *config.File, error) {
	store, err := resolve()
	if err != nil {
		return nil, nil, exitcode.New(exitcode.RuntimeErr, err)
	}
	file, err := store.Load()
	if err != nil {
		return nil, nil, exitcode.New(exitcode.RuntimeErr, err)
	}
	return store, file, nil
}

func newSetContext(resolve StoreResolver) *cobra.Command {
	var baseURL, team, project string
	cmd := &cobra.Command{
		Use:   "set-context NAME",
		Short: "Create or update a named context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, file, err := loadStore(resolve)
			if err != nil {
				return err
			}
			name := args[0]

			// Merge with any existing context so unspecified fields are kept.
			ctx := config.Context{Name: name}
			if existing := file.Find(name); existing != nil {
				ctx = *existing
			}
			if cmd.Flags().Changed("base-url") {
				ctx.BaseURL = baseURL
			}
			if cmd.Flags().Changed("team") {
				ctx.DefaultTeam = team
			}
			if cmd.Flags().Changed("project") {
				ctx.DefaultProject = project
			}
			file.Upsert(ctx)

			// First context created becomes the active one.
			if file.CurrentContext == "" {
				file.CurrentContext = name
			}
			if err := store.Save(file); err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			cmd.PrintErrf("Context %q set.\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&baseURL, "base-url", "", "base URL for the context")
	cmd.Flags().StringVar(&team, "team", "", "default team id or slug")
	cmd.Flags().StringVar(&project, "project", "", "default project id or slug")
	return cmd
}

func newUseContext(resolve StoreResolver) *cobra.Command {
	return &cobra.Command{
		Use:   "use-context NAME",
		Short: "Switch the active context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, file, err := loadStore(resolve)
			if err != nil {
				return err
			}
			name := args[0]
			if file.Find(name) == nil {
				return exitcode.Usage("no context named %q", name)
			}
			file.CurrentContext = name
			if err := store.Save(file); err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			cmd.PrintErrf("Switched to context %q.\n", name)
			return nil
		},
	}
}

func newGetContexts(resolve StoreResolver) *cobra.Command {
	return &cobra.Command{
		Use:   "get-contexts",
		Short: "List all contexts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, file, err := loadStore(resolve)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "CURRENT\tNAME\tBASE URL\tTEAM\tPROJECT")
			for _, c := range file.Contexts {
				marker := ""
				if c.Name == file.CurrentContext {
					marker = "*"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					marker, c.Name, c.BaseURL, c.DefaultTeam, c.DefaultProject)
			}
			return w.Flush()
		},
	}
}

func newCurrentContext(resolve StoreResolver, getenv config.Getenv) *cobra.Command {
	return &cobra.Command{
		Use:   "current-context",
		Short: "Show the active context (after flag/env/config precedence)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, file, err := loadStore(resolve)
			if err != nil {
				return err
			}
			// current-context reflects env precedence too (VIBEXP_CONTEXT), but
			// not the transient --context flag (that would be surprising here).
			rt := file.Resolve(config.Overrides{}, getenv)
			if rt.ContextName == "" {
				return exitcode.New(exitcode.RuntimeErr, fmt.Errorf("no active context set"))
			}
			cmd.Println(rt.ContextName)
			return nil
		},
	}
}
