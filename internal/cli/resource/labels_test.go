package resource

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestLabelFlagsAddTo(t *testing.T) {
	var ten, tenArgs []string
	for i := 0; i < MaxLabels; i++ {
		ten = append(ten, "l"+strings.Repeat("x", i))
		tenArgs = append(tenArgs, "--label", ten[i])
	}
	with := func(extra ...string) []string { return append(append([]string{}, tenArgs...), extra...) }
	tests := []struct {
		name string
		args []string
		want any  // payload["labels"]; nil = key absent
		bad  bool // flag parsing must fail
	}{
		{"not given sends nothing", nil, nil, false},
		{"repeatable in order", []string{"--label", "a", "--label", "b"}, []string{"a", "b"}, false},
		{"blank clears", []string{"--label", ""}, []string{}, false},
		{"blank values dropped", []string{"--label", " ", "--label", "a"}, []string{"a"}, false},
		{"50 multibyte runes ok", []string{"--label", strings.Repeat("é", MaxLabelLength)}, []string{strings.Repeat("é", MaxLabelLength)}, false},
		{"51 characters rejected", []string{"--label", strings.Repeat("a", MaxLabelLength+1)}, nil, true},
		{"11 labels rejected", with("--label", "one-too-many"), nil, true},
		{"blanks do not count toward the cap", with("--label", " "), ten, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "create"}
			var f LabelFlags
			AddLabelFlags(cmd, &f, false)
			err := cmd.ParseFlags(tc.args)
			if (err != nil) != tc.bad {
				t.Fatalf("parse error = %v, want error %v", err, tc.bad)
			}
			if tc.bad {
				return
			}
			payload := map[string]any{}
			f.AddTo(payload)
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
	u := cmd.Flags().Lookup("label").Usage
	if !strings.Contains(u, "replaces") || !strings.Contains(u, "at most 10, 50 characters") {
		t.Errorf("update --label usage = %q, want the replace semantics and the limits", u)
	}
}
