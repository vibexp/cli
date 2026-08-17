package resource

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestListFiltersQueryMergesAndSorts(t *testing.T) {
	f := &ListFilters{Tags: true}
	f.pairs = []string{"env=prod", "env=staging", "team=core"}
	f.tags = []string{"go"}
	got, err := f.Query()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"env":["prod","staging"],"tags":["go"],"team":["core"]}`
	if got != want {
		t.Errorf("Query() = %q, want %q", got, want)
	}
}

func TestListFiltersQueryEmptyWhenUnset(t *testing.T) {
	got, err := (&ListFilters{}).Query()
	if err != nil || got != "" {
		t.Errorf("Query() = %q, %v; want empty", got, err)
	}
}

func TestListFiltersQueryEmptyValueMeansKeyExists(t *testing.T) {
	f := &ListFilters{}
	f.pairs = []string{"deprecated="}
	got, err := f.Query()
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"deprecated":[""]}` {
		t.Errorf("Query() = %q", got)
	}
}

func TestListFiltersQueryRejectsMissingEquals(t *testing.T) {
	f := &ListFilters{}
	f.pairs = []string{"noequals"}
	if _, err := f.Query(); err == nil {
		t.Error("expected usage error for pair without =")
	}
}

func TestListFiltersApplyToPathMergesQuery(t *testing.T) {
	f := &ListFilters{}
	f.pairs = []string{"env=prod"}
	got, err := f.ApplyToPath("/api/v1/t/memories?project_id=p-1")
	if err != nil {
		t.Fatal(err)
	}
	want := "/api/v1/t/memories?metadata=%7B%22env%22%3A%5B%22prod%22%5D%7D&project_id=p-1"
	if got != want {
		t.Errorf("ApplyToPath() = %q, want %q", got, want)
	}
}

// TestListFiltersApplyToPathStale covers the v0.11.0 freshness filter across
// the shapes a list path actually takes: alone, composed with every other
// filter, unset, and merged into a path that already carries project scope.
func TestListFiltersApplyToPathStale(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*ListFilters)
		path  string
		want  string
	}{
		{
			name:  "stale alone",
			setup: func(f *ListFilters) { f.stale = true },
			path:  "/api/v1/t/memories",
			want:  "/api/v1/t/memories?freshness=stale",
		},
		{
			name:  "unset adds nothing",
			setup: func(*ListFilters) {},
			path:  "/api/v1/t/memories?limit=10",
			want:  "/api/v1/t/memories?limit=10",
		},
		{
			name: "stale composes with metadata, tags and pagination",
			setup: func(f *ListFilters) {
				f.stale = true
				f.pairs = []string{"env=prod"}
				f.tags = []string{"go"}
			},
			path: "/api/v1/t/memories?limit=10",
			want: "/api/v1/t/memories?freshness=stale" +
				"&limit=10&metadata=%7B%22env%22%3A%5B%22prod%22%5D%2C%22tags%22%3A%5B%22go%22%5D%7D",
		},
		{
			name:  "stale merges into a project-scoped path",
			setup: func(f *ListFilters) { f.stale = true },
			path:  "/api/v1/t/artifacts?project_id=p-1",
			want:  "/api/v1/t/artifacts?freshness=stale&project_id=p-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &ListFilters{Metadata: true, Tags: true, Stale: true}
			tt.setup(f)
			got, err := f.ApplyToPath(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("ApplyToPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestListFiltersApplyToPathUnsetIsByteIdentical guards the criterion that an
// unfiltered list never grows an empty param: url.Parse + Encode would
// normalise the query, so the early return has to skip it entirely.
func TestListFiltersApplyToPathUnsetIsByteIdentical(t *testing.T) {
	path := "/api/v1/t/memories?b=2&a=1"
	got, err := (&ListFilters{Metadata: true, Tags: true, Stale: true}).ApplyToPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("ApplyToPath() = %q, want the path unchanged (%q)", got, path)
	}
}

// TestAddFilterFlagsBindsOnlyOptedIn is the guard against handing a command a
// filter its endpoint ignores — the failure the server's strict enum warns
// about, one level up.
func TestAddFilterFlagsBindsOnlyOptedIn(t *testing.T) {
	tests := []struct {
		name    string
		filters ListFilters
		want    map[string]bool // flag name -> should be bound
	}{
		{
			name:    "prompts: stale only",
			filters: ListFilters{Stale: true},
			want:    map[string]bool{"stale": true, "metadata": false, "tags": false},
		},
		{
			name:    "artifacts and blueprints: metadata + stale",
			filters: ListFilters{Metadata: true, Stale: true},
			want:    map[string]bool{"stale": true, "metadata": true, "tags": false},
		},
		{
			name:    "memories: everything",
			filters: ListFilters{Metadata: true, Tags: true, Stale: true},
			want:    map[string]bool{"stale": true, "metadata": true, "tags": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "list"}
			f := tt.filters
			AddFilterFlags(cmd, &f)
			for name, wantBound := range tt.want {
				if bound := cmd.Flags().Lookup(name) != nil; bound != wantBound {
					t.Errorf("--%s bound = %v, want %v", name, bound, wantBound)
				}
			}
		})
	}
}
