package atomicfile

import (
	"encoding/json"
	"io/fs"
	"os"
)

// ReadJSON decodes the JSON file at path. It reports false, with the zero
// value, when the file is missing, unreadable, or does not decode.
func ReadJSON[T any](path string) (T, bool) {
	var zero T
	raw, err := os.ReadFile(path)
	if err != nil {
		return zero, false
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return zero, false
	}
	return v, true
}

// WriteJSON encodes v and replaces path with it through WriteFile.
func WriteJSON(path string, v any, perm fs.FileMode) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteFile(path, raw, perm)
}
