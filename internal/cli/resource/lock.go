package resource

import (
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// The v0.15.0 resource lock (platform epic #1285) lives on prompts, blueprints
// and artifacts, so its columns and the lock/unlock verbs are declared once
// here, like the freshness columns, rather than copied into each noun package.
//
// The `lock` object is null while a resource is unlocked. Every column below
// therefore renders an empty cell on an unlocked resource: the renderer does so
// for a missing path, and LockColumn is guarded so a null never reads "false".

// LockColumn is the compact list-view flag: "locked", or empty when unlocked.
// It goes after UPDATED so the TSV field positions scripts rely on stay put.
func LockColumn() output.Column {
	return output.Column{Header: "LOCKED", Path: `if .lock then "locked" else "" end`}
}

// LockDetailColumns is the full lock for single-object views: the flag plus
// who holds it, since when, and why.
func LockDetailColumns() []output.Column {
	return []output.Column{
		LockColumn(),
		{Header: "LOCKED_BY", Path: ".lock.locked_by.display_name"},
		{Header: "LOCKED_AT", Path: ".lock.locked_at"},
		{Header: "LOCK_COMMENT", Path: ".lock.comment"},
	}
}

// ItemPathFunc builds a lockable noun's single-item path for a slug, resolving
// the team (and, for project-scoped nouns, the project; missing → exit 2).
type ItemPathFunc func(rt *config.Runtime, slug string) (string, error)

// NewLockCommand builds `<noun> lock <slug> --comment "<why>"`: POST
// …/{slug}/lock, rendering the locked resource with spec.
func NewLockCommand(noun string, resolve CredResolver, getenv config.Getenv, itemPath ItemPathFunc, spec output.TableSpec) *cobra.Command {
	var comment string
	cmd := &cobra.Command{
		Use:   "lock <slug>",
		Short: "Lock a " + noun + " to mark it as in use",
		Long: "Lock a " + noun + " so it cannot be deleted, renamed or moved; its content\n" +
			"stays editable. --comment says why (required). Only the lock holder, or an\n" +
			"owner/admin, can unlock it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			comment = strings.TrimSpace(comment)
			if comment == "" {
				return exitcode.Usage("--comment is required: say why the %s is locked", noun)
			}
			return sendLock(cmd, resolve, getenv, itemPath, args[0], http.MethodPost, map[string]string{"comment": comment}, &spec)
		},
	}
	cmd.Flags().StringVar(&comment, "comment", "", "why the "+noun+" is locked (required)")
	return cmd
}

// NewUnlockCommand builds `<noun> unlock <slug>`: DELETE …/{slug}/lock,
// rendering the unlocked resource with spec.
func NewUnlockCommand(noun string, resolve CredResolver, getenv config.Getenv, itemPath ItemPathFunc, spec output.TableSpec) *cobra.Command {
	return &cobra.Command{
		Use:   "unlock <slug>",
		Short: "Release the lock on a " + noun,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendLock(cmd, resolve, getenv, itemPath, args[0], http.MethodDelete, nil, &spec)
		},
	}
}

// sendLock is the shared body of lock and unlock. The client is built before
// the path so a slug --team/--project resolves through the runtime's resolver.
func sendLock(cmd *cobra.Command, resolve CredResolver, getenv config.Getenv, itemPath ItemPathFunc, slug, method string, payload any, spec *output.TableSpec) error {
	ctx, rt, client, err := RuntimeAndClient(cmd, resolve, getenv)
	if err != nil {
		return err
	}
	path, err := itemPath(rt, slug)
	if err != nil {
		return err
	}
	if payload != nil {
		return SendItem(ctx, cmd, rt, getenv, client, method, path+"/lock", payload, spec)
	}
	raw, _, err := Do(ctx, client, method, path+"/lock", nil)
	if err != nil {
		return err
	}
	return Render(cmd, rt, getenv, raw, spec)
}
