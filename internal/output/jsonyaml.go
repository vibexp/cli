package output

import (
	"fmt"
	"io"

	"sigs.k8s.io/yaml"
)

// renderJSON writes the API response body unchanged — the JSON contract is
// byte-identical to what the server sent (no CLI mapping layer).
func renderJSON(w io.Writer, raw []byte) error {
	_, err := w.Write(raw)
	return err
}

// renderYAML writes a faithful YAML conversion of the JSON body.
func renderYAML(w io.Writer, raw []byte) error {
	out, err := yaml.JSONToYAML(raw)
	if err != nil {
		return fmt.Errorf("convert response to YAML: %w", err)
	}
	_, err = w.Write(out)
	return err
}
