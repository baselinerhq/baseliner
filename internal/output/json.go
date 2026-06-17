package output

import (
	"bytes"
	"encoding/json"
	"io"
	"os"

	"github.com/baselinerhq/baseliner/internal/models"
)

// marshalJSON renders the result with 2-space indent and without HTML escaping,
// matching pydantic's model_dump_json(indent=2). No trailing newline.
func marshalJSON(r *models.RunResult) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// WriteJSON writes the result as JSON. When path is empty it writes to stdout with
// a trailing newline; otherwise it writes atomically to path (tmp file + rename)
// with no trailing newline.
func WriteJSON(stdout io.Writer, r *models.RunResult, path string) error {
	content, err := marshalJSON(r)
	if err != nil {
		return err
	}
	if path == "" {
		if _, err := stdout.Write(content); err != nil {
			return err
		}
		_, err := stdout.Write([]byte("\n"))
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
