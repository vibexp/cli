package resource

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

// The platform's per-resource label limits (v0.13.0 shared labels): a resource
// carries at most MaxLabels labels of at most MaxLabelLength characters each.
// They are checked as the flag is parsed, so an oversized list is a usage
// error before anything is read or sent, not a round trip ending in a 400.
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
	limits := fmt.Sprintf("at most %d, %d characters each", MaxLabels, MaxLabelLength)
	usage := "label for categorizing and filtering (repeatable; " + limits + ")"
	if update {
		usage = "replacement label (repeatable; " + limits + "; replaces all existing labels, --label \"\" clears them)"
	}
	cmd.Flags().Var(labelValue{&f.vals}, "label", usage)
}

// Set reports whether --label was given, even as "".
func (f *LabelFlags) Set() bool {
	return f.cmd != nil && f.cmd.Flags().Changed("label")
}

// AddTo sets payload["labels"] when --label was given; --label "" alone sends
// an empty list, which clears the labels on update.
func (f *LabelFlags) AddTo(payload map[string]any) {
	if f.Set() {
		payload["labels"] = append([]string{}, f.vals...)
	}
}

// labelValue is the pflag.Value behind --label: a string array that drops
// blank values and enforces the platform's limits as each value is parsed.
type labelValue struct{ vals *[]string }

// String is "" when unset so --help prints no "(default [])": pflag only
// recognises "[]" as a zero default for its own array types.
func (v labelValue) String() string {
	if len(*v.vals) == 0 {
		return ""
	}
	return "[" + strings.Join(*v.vals, ",") + "]"
}

func (v labelValue) Type() string { return "stringArray" }

func (v labelValue) Set(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if n := utf8.RuneCountInString(s); n > MaxLabelLength {
		return fmt.Errorf("at most %d characters per label, got %d", MaxLabelLength, n)
	}
	if len(*v.vals) == MaxLabels {
		return fmt.Errorf("at most %d labels", MaxLabels)
	}
	*v.vals = append(*v.vals, s)
	return nil
}
