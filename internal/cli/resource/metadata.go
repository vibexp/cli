package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/exitcode"
)

// MetadataHelp is the shared Long-help paragraph for every command that binds
// the metadata write flags. The memory tags caveat is stated for every noun:
// it is harmless where it does not apply and it keeps the three helps aligned.
const MetadataHelp = "Metadata is set with --metadata key=value (repeatable; values are\n" +
	"strings) and/or --metadata-json '<object>' (inline JSON, @file, or '-' for\n" +
	"stdin) for numbers, bools, arrays and nested objects; --metadata wins on a\n" +
	"key conflict. On memories, a \"tags\" key holding a JSON string array is\n" +
	"turned by the server into labels (replacing the memory's existing labels)\n" +
	"and is not stored as metadata; a plain --metadata tags=x stays a string key."

// MetadataUpdateHelp adds the update-only semantics to MetadataHelp.
const MetadataUpdateHelp = MetadataHelp + "\n\n" +
	"On update the given keys are merged into the existing metadata (the\n" +
	"resource is read first; a concurrent write between that read and this\n" +
	"update is lost). --unset-metadata <key> removes a key; --replace-metadata\n" +
	"sends only the given keys, replacing the whole object without a read."

// MetadataFlags binds the metadata write flags of a create/update command. The
// server replaces the whole metadata object whenever one is sent, so update
// merges into the current object unless --replace-metadata says otherwise.
type MetadataFlags struct {
	pairs   []string
	jsonArg string
	unset   []string
	replace bool

	// cmd is the command the flags are bound to: an explicitly empty
	// --metadata-json is an error, not "unset", so presence is read from
	// cobra rather than from the value.
	cmd *cobra.Command
}

// AddMetadataFlags binds --metadata and --metadata-json onto cmd, plus
// --unset-metadata and --replace-metadata when update is true.
func AddMetadataFlags(cmd *cobra.Command, f *MetadataFlags, update bool) {
	f.cmd = cmd
	cmd.Flags().StringArrayVar(&f.pairs, "metadata", nil,
		"set a metadata key as key=value (repeatable; value is a string)")
	cmd.Flags().StringVar(&f.jsonArg, "metadata-json", "",
		"metadata as a JSON object: inline, @file, or '-' for stdin")
	if update {
		cmd.Flags().StringArrayVar(&f.unset, "unset-metadata", nil,
			"remove a metadata key (repeatable)")
		cmd.Flags().BoolVar(&f.replace, "replace-metadata", false,
			"replace the whole metadata object with the given keys instead of merging")
	}
}

// Set reports whether any metadata flag was given.
func (f *MetadataFlags) Set() bool {
	return len(f.pairs) > 0 || f.jsonGiven() || len(f.unset) > 0 || f.replace
}

// jsonGiven reports whether --metadata-json was passed, even as "".
func (f *MetadataFlags) jsonGiven() bool {
	return f.jsonArg != "" || (f.cmd != nil && f.cmd.Flags().Changed("metadata-json"))
}

// CheckStdin rejects reading both the body and the metadata JSON from stdin,
// which would hand the second reader an empty stream.
func (f *MetadataFlags) CheckStdin(bodyFile string) error {
	if bodyFile == "-" && f.jsonArg == "-" {
		return exitcode.Usage("--body-file and --metadata-json cannot both read stdin ('-')")
	}
	return nil
}

// Build returns the metadata object the flags describe: --metadata-json first,
// then the --metadata pairs overlaid on it. in is read when --metadata-json is
// '-'. Every malformed input is a usage error.
func (f *MetadataFlags) Build(in io.Reader) (map[string]any, error) {
	out := map[string]any{}
	if f.jsonGiven() {
		obj, err := parseMetadataJSON(f.jsonArg, in)
		if err != nil {
			return nil, err
		}
		out = obj
	}
	for _, p := range f.pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, exitcode.Usage("invalid --metadata %q: expected key=value", p)
		}
		out[k] = v
	}
	if f.replace && len(f.unset) > 0 {
		return nil, exitcode.Usage("--replace-metadata and --unset-metadata are mutually exclusive")
	}
	for _, k := range f.unset {
		if k == "" {
			return nil, exitcode.Usage("invalid --unset-metadata: key is empty")
		}
		if _, ok := out[k]; ok {
			return nil, exitcode.Usage("metadata key %q is both set and unset", k)
		}
	}
	return out, nil
}

// ForUpdate returns the metadata object an update should send, reading the
// resource at itemPath first unless --replace-metadata is given. The flags are
// validated before any request, so malformed input never reaches the server.
func (f *MetadataFlags) ForUpdate(ctx context.Context, client *api.RawClient, itemPath string, in io.Reader) (map[string]any, error) {
	given, err := f.Build(in)
	if err != nil {
		return nil, err
	}
	if f.replace {
		return given, nil
	}
	raw, err := FetchJSON(ctx, client, http.MethodGet, itemPath)
	if err != nil {
		return nil, err
	}
	var item struct {
		Metadata map[string]any `json:"metadata"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // round-trip numbers untouched
	if err := dec.Decode(&item); err != nil {
		return nil, exitcode.New(exitcode.RuntimeErr, err)
	}
	return f.overlay(item.Metadata, given), nil
}

// overlay applies already-built flag values to current (merge mode only).
func (f *MetadataFlags) overlay(current, given map[string]any) map[string]any {
	out := make(map[string]any, len(current)+len(given))
	for k, v := range current {
		out[k] = v
	}
	for k, v := range given {
		out[k] = v
	}
	for _, k := range f.unset {
		delete(out, k)
	}
	return out
}

// AddTo sets payload["metadata"] for a create when a metadata flag was given;
// stdin is the bound command's.
func (f *MetadataFlags) AddTo(payload map[string]any) error {
	if !f.Set() {
		return nil
	}
	m, err := f.Build(f.cmd.InOrStdin())
	if err != nil {
		return err
	}
	payload["metadata"] = m
	return nil
}

// AddMerged sets payload["metadata"] for an update when a metadata flag was
// given, merging into the item at itemPath (see ForUpdate).
func (f *MetadataFlags) AddMerged(ctx context.Context, client *api.RawClient, itemPath string, payload map[string]any) error {
	if !f.Set() {
		return nil
	}
	m, err := f.ForUpdate(ctx, client, itemPath, f.cmd.InOrStdin())
	if err != nil {
		return err
	}
	payload["metadata"] = m
	return nil
}

// parseMetadataJSON reads --metadata-json (inline, @file, or '-') and requires
// a JSON object.
func parseMetadataJSON(arg string, in io.Reader) (map[string]any, error) {
	var data []byte
	switch {
	case arg == "-":
		b, err := io.ReadAll(in)
		if err != nil {
			return nil, exitcode.New(exitcode.RuntimeErr, err)
		}
		data = b
	case strings.HasPrefix(arg, "@"):
		b, err := os.ReadFile(arg[1:])
		if err != nil {
			return nil, exitcode.Usage("read --metadata-json %q: %v", arg[1:], err)
		}
		data = b
	default:
		data = []byte(arg)
	}
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, exitcode.Usage("invalid --metadata-json: expected a JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, exitcode.Usage("invalid --metadata-json: trailing data after the JSON object")
	}
	return obj, nil
}
