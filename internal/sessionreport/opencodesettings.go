package sessionreport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// opencodeCLIConfig reads, without writing, the CLI settings opencode would
// start from: the inline variable when the user sets one, with the file's
// plugin list under it when the variable names none, since opencode takes
// a list from the variable over the file's and merges the rest.
func opencodeCLIConfig() (map[string]json.RawMessage, error) {
	config := map[string]json.RawMessage{}
	if inline := os.Getenv(opencodeCLIConfigVar); inline != "" {
		if err := json.Unmarshal([]byte(inline), &config); err != nil {
			return nil, fmt.Errorf("%s: %w", opencodeCLIConfigVar, err)
		}
		if config == nil {
			config = map[string]json.RawMessage{}
		}
		if _, listed := config["plugins"]; listed {
			return config, nil
		}
	}
	path, err := opencodeCLIConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return nil, err
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(stripJSONTrailingCommas(stripJSONComments(data)), &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if raw, listed := file["plugins"]; listed {
		config["plugins"] = raw
	}
	return config, nil
}

// opencodeCLIConfigPath follows opencode's own lookup for cli.json.
func opencodeCLIConfigPath() (string, error) {
	if dir := os.Getenv("OPENCODE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "cli.json"), nil
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "opencode", "cli.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "cli.json"), nil
}

func fileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

// stripJSONTrailingCommas blanks a comma that closes a list or object,
// which opencode's own reader of cli.json allows.
func stripJSONTrailingCommas(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	inString, escaped := false, false
	for i := 0; i < len(out); i++ {
		switch {
		case inString:
			switch {
			case escaped:
				escaped = false
			case out[i] == '\\':
				escaped = true
			case out[i] == '"':
				inString = false
			}
		case out[i] == '"':
			inString = true
		case out[i] == ',':
			next := i + 1
			for next < len(out) && (out[next] == ' ' || out[next] == '\t' || out[next] == '\n' || out[next] == '\r') {
				next++
			}
			if next < len(out) && (out[next] == ']' || out[next] == '}') {
				out[i] = ' '
			}
		}
	}
	return out
}
