package resource

import (
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/exitcode"
)

// The platform's per-resource label limits (v0.13.0 shared labels): a resource
// carries at most MaxLabels labels of at most MaxLabelLength characters each.
// They are checked before any request so an oversized list is a usage error,
// not a round trip ending in a 400.
const (
	MaxLabels      = 10
	MaxLabelLength = 50
)

// LabelFlags binds the repeatable --label write flag of a memory, artifact or
// blueprint create/update. Labels are sent only when --label was given, so an
// update without it never touches (let alone clears) the resource's labels.
type LabelFlags struct {
	vals []string

	// cmd is the command the flag is bound to: presence, not value, decides
	// whether labels are sent, so --label "" can clear them on update.
	cmd *cobra.Command
}

// AddLabelFlags binds --label onto cmd. On update the given labels replace the
// resource's existing ones.
func AddLabelFlags(cmd *cobra.Command, f *LabelFlags, update bool) {
	f.cmd = cmd
	usage := "label for categorizing and filtering (repeatable; at most 10, 50 characters each)"
	if update {
		usage = "replacement label (repeatable; replaces all existing labels, --label \"\" clears them)"
	}
	cmd.Flags().StringArrayVar(&f.vals, "label", nil, usage)
}

// Set reports whether --label was given, even as "".
func (f *LabelFlags) Set() bool {
	return f.cmd != nil && f.cmd.Flags().Changed("label")
}

// AddTo sets payload["labels"] when --label was given. Blank values are
// dropped, so --label "" alone sends an empty list. Beyond the platform's
// limits it is a usage error and nothing is sent.
func (f *LabelFlags) AddTo(payload map[string]any) error {
	if !f.Set() {
		return nil
	}
	labels := make([]string, 0, len(f.vals))
	for _, v := range f.vals {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if n := utf8.RuneCountInString(v); n > MaxLabelLength {
			return exitcode.Usage("invalid --label %q: at most %d characters, got %d", v, MaxLabelLength, n)
		}
		labels = append(labels, v)
	}
	if len(labels) > MaxLabels {
		return exitcode.Usage("too many --label values: at most %d, got %d", MaxLabels, len(labels))
	}
	payload["labels"] = labels
	return nil
}
