package resource

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/output"
)

// relationsSummaryMax caps how many entries each summary line names before
// collapsing the rest into a "+N more" tail.
const relationsSummaryMax = 5

// AddRelationsFlag binds --show-relations to a `get` command and returns the
// bound pointer. When set, the command prints a compact related/similar summary
// (the v0.8.0 read-time neighborhood) to stderr after rendering the resource.
// The full arrays always remain available via --format=json.
func AddRelationsFlag(cmd *cobra.Command) *bool {
	show := new(bool)
	cmd.Flags().BoolVar(show, "show-relations", false,
		"print a compact related/similar summary to stderr (v0.8.0 read fields)")
	return show
}

// RenderWithRelations renders a single-resource body through the output engine,
// then — when showRelations is set — prints the v0.8.0 related/similar summary
// to stderr. It is the shared tail of every resource `get` command.
func RenderWithRelations(cmd *cobra.Command, rt *config.Runtime, getenv config.Getenv, body []byte, spec *output.TableSpec, showRelations bool) error {
	if err := Render(cmd, rt, getenv, body, spec); err != nil {
		return err
	}
	if showRelations {
		RenderRelationsSummary(cmd, body)
	}
	return nil
}

type relatedItem struct {
	RelationType string `json:"relation_type"`
	Direction    string `json:"direction"`
	ResourceType string `json:"resource_type"`
	Title        string `json:"title"`
}

type similarItem struct {
	Type  string  `json:"type"`
	Title string  `json:"title"`
	Score float64 `json:"score"`
}

type relationsEnvelope struct {
	Related []relatedItem `json:"related"`
	Similar []similarItem `json:"similar"`
}

// RenderRelationsSummary parses the optional v0.8.0 `related`/`similar` arrays
// from a resource GET body and writes a compact, human summary to stderr — so
// stdout stays data-only. It is a no-op when both arrays are absent or empty,
// and never fails the command: a body that does not parse is simply skipped,
// since these arrays are best-effort read-time context, not the resource itself.
func RenderRelationsSummary(cmd *cobra.Command, body []byte) {
	var env relationsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	if len(env.Related) > 0 {
		parts := make([]string, 0, len(env.Related))
		for i, r := range env.Related {
			if i == relationsSummaryMax {
				parts = append(parts, fmt.Sprintf("… +%d more", len(env.Related)-relationsSummaryMax))
				break
			}
			parts = append(parts, fmt.Sprintf("%s %s %s %q", r.Direction, r.RelationType, r.ResourceType, r.Title))
		}
		cmd.PrintErrf("related (%d): %s\n", len(env.Related), strings.Join(parts, "; "))
	}
	if len(env.Similar) > 0 {
		parts := make([]string, 0, len(env.Similar))
		for i, s := range env.Similar {
			if i == relationsSummaryMax {
				parts = append(parts, fmt.Sprintf("… +%d more", len(env.Similar)-relationsSummaryMax))
				break
			}
			parts = append(parts, fmt.Sprintf("%s %q (%.2f)", s.Type, s.Title, s.Score))
		}
		cmd.PrintErrf("similar (%d): %s\n", len(env.Similar), strings.Join(parts, "; "))
	}
}
