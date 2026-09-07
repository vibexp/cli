package resource_test

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/output"
)

func listCfg() resource.ListConfig {
	return resource.ListConfig{
		PathFor: func(_ *config.Runtime) (string, error) { return "/api/v1/things", nil },
		Spec:    output.TableSpec{Rows: resource.ListRows("things")},
	}
}

// flagNames is the pagination surface every list command must bind, whatever
// verb it is registered under.
var flagNames = []string{"limit", "page", "offset"}

func assertListCommand(t *testing.T, cmd *cobra.Command, wantUse, wantShort string) {
	t.Helper()
	if cmd.Use != wantUse {
		t.Errorf("Use = %q, want %q", cmd.Use, wantUse)
	}
	if cmd.Short != wantShort {
		t.Errorf("Short = %q, want %q", cmd.Short, wantShort)
	}
	if cmd.RunE == nil {
		t.Error("RunE not wired")
	}
	for _, f := range flagNames {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("pagination flag --%s not bound", f)
		}
	}
	// Args is NoArgs: a list verb takes no positional argument.
	if err := cmd.Args(cmd, []string{"stray"}); err == nil {
		t.Error("a positional argument should be rejected")
	}
}

func TestNewListCommandIsNamedList(t *testing.T) {
	assertListCommand(t, resource.NewListCommand(nil, nil, "List things", listCfg()), "list", "List things")
}

// NewNamedListCommand exists so a noun can expose a second paginated
// collection (e.g. `team audit` beside `team list`); only the verb differs.
func TestNewNamedListCommandUsesTheGivenVerb(t *testing.T) {
	assertListCommand(t, resource.NewNamedListCommand("audit", nil, nil, "Audit things", listCfg()), "audit", "Audit things")
}

func TestNamedListCommandBindsFilterFlagsWhenOptedIn(t *testing.T) {
	cfg := listCfg()
	cfg.Filters = &resource.ListFilters{Metadata: true, Tags: true, Stale: true}
	cmd := resource.NewNamedListCommand("audit", nil, nil, "Audit things", cfg)
	for _, f := range []string{"metadata", "tags", "stale"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("filter flag --%s not bound", f)
		}
	}
	// A nil Filters must bind none of them.
	plain := resource.NewNamedListCommand("audit", nil, nil, "Audit things", listCfg())
	for _, f := range []string{"metadata", "tags", "stale"} {
		if plain.Flags().Lookup(f) != nil {
			t.Errorf("--%s bound without opting in", f)
		}
	}
}
