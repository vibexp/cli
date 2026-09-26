package resource

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/exitcode"
)

func TestLabelFlagsAddTo(t *testing.T) {
	eleven := make([]string, 0, 22)
	for i := 0; i <= MaxLabels; i++ {
		eleven = append(eleven, "--label", "l"+strings.Repeat("x", i))
	}
	tests := []struct {
		name string
		args []string
		want any // payload["labels"]; nil = key absent
		code int
	}{
		{"not given sends nothing", nil, nil, 0},
		{"repeatable in order", []string{"--label", "a", "--label", "b"}, []string{"a", "b"}, 0},
		{"blank clears", []string{"--label", ""}, []string{}, 0},
		{"blank values dropped", []string{"--label", " ", "--label", "a"}, []string{"a"}, 0},
		{"50 multibyte runes ok", []string{"--label", strings.Repeat("é", MaxLabelLength)}, []string{strings.Repeat("é", MaxLabelLength)}, 0},
		{"51 characters rejected", []string{"--label", strings.Repeat("a", MaxLabelLength+1)}, nil, exitcode.UsageErr},
		{"11 labels rejected", eleven, nil, exitcode.UsageErr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "create"}
			var f LabelFlags
			AddLabelFlags(cmd, &f, false)
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			payload := map[string]any{}
			err := f.AddTo(payload)
			if code := exitcode.FromError(err); code != tc.code {
				t.Fatalf("exit = %d, want %d (%v)", code, tc.code, err)
			}
			got, ok := payload["labels"]
			if tc.want == nil {
				if ok {
					t.Errorf("labels = %v, want the key absent", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("labels = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestAddLabelFlagsUpdateUsage(t *testing.T) {
	cmd := &cobra.Command{Use: "update"}
	AddLabelFlags(cmd, &LabelFlags{}, true)
	if u := cmd.Flags().Lookup("label").Usage; !strings.Contains(u, "replaces") {
		t.Errorf("update --label usage = %q, want it to say labels are replaced", u)
	}
}
