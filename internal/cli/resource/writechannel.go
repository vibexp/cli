package resource

import "github.com/vibexp/cli/internal/output"

// The v0.16.0 write channel (platform #1394, #1450) is recorded on every
// curated resource — prompts, memories, blueprints and artifacts — so its
// columns are declared once here rather than copied into each noun package.
//
// Both fields are server-derived and nullable: null means the channel was not
// recorded (a write before v0.16.0, or a pre-v0.16.0 server that omits the
// fields entirely). The renderer turns both null and a missing path into an
// empty cell (see internal/output/extract.go), so the CLI never guesses one.

// WriteChannelDetailColumns is the write provenance for single-object views:
// the channel of the last write (web, cli, mcp or api) and, for MCP writes
// only, the name the MCP client reported. They go last in a detail spec, so
// the columns before them keep their positions for scripts. List views leave
// them out; they are not worth the width there.
func WriteChannelDetailColumns() []output.Column {
	return []output.Column{
		{Header: "WRITTEN_VIA", Path: ".last_write_source"},
		{Header: "ASSISTANT", Path: ".last_write_assistant_name"},
	}
}
