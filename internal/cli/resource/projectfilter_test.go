package resource_test

import (
	"testing"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
)

func TestWithProjectFilter(t *testing.T) {
	const project = "2c3d4e5f-6a7b-4c8d-9e0f-1a2b3c4d5e6f"
	for _, tc := range []struct{ project, want string }{
		{"", "/api/v1/t/memories"},
		{project, "/api/v1/t/memories?project_id=" + project},
	} {
		got, err := resource.WithProjectFilter("/api/v1/t/memories", &config.Runtime{Project: tc.project})
		if err != nil || got != tc.want {
			t.Errorf("WithProjectFilter(project %q) = %q, %v; want %q", tc.project, got, err, tc.want)
		}
	}
}
