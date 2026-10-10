package sessionreport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// geminiTelemetryInUse reads, without writing, whether the user already has
// gemini's telemetry on: through a GEMINI_TELEMETRY_ variable in the
// environment or in the .env file gemini loads, or through
// telemetry.enabled in any settings file gemini loads for cwd. The paths
// follow gemini-cli's own loader.
func geminiTelemetryInUse(cwd string) (bool, error) {
	for _, variable := range os.Environ() {
		if strings.HasPrefix(variable, "GEMINI_TELEMETRY_") {
			return true, nil
		}
	}
	if inUse, err := geminiEnvFileSetsTelemetry(cwd); err != nil || inUse {
		return inUse, err
	}
	paths, err := geminiSettingsPaths(cwd)
	if err != nil {
		return false, err
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		var settings struct {
			Telemetry struct {
				Enabled bool `json:"enabled"`
			} `json:"telemetry"`
		}
		if err := json.Unmarshal(stripJSONComments(data), &settings); err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
		if settings.Telemetry.Enabled {
			return true, nil
		}
	}
	return false, nil
}

// geminiEnvFileSetsTelemetry reads the .env file gemini would load for cwd:
// the nearest .gemini/.env or .env walking up from cwd, then the home
// directory's. gemini loads .gemini/.env only for a trusted folder, which
// is not known here, so both files at the level gemini stops at count.
func geminiEnvFileSetsTelemetry(cwd string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		found, inUse, err := envFilesSetTelemetry(filepath.Join(dir, ".gemini", ".env"), filepath.Join(dir, ".env"))
		if err != nil || found {
			return inUse, err
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	_, inUse, err := envFilesSetTelemetry(filepath.Join(home, ".gemini", ".env"), filepath.Join(home, ".env"))
	return inUse, err
}

func envFilesSetTelemetry(paths ...string) (found, inUse bool, err error) {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return true, false, err
		}
		found = true
		for line := range strings.SplitSeq(string(data), "\n") {
			line = strings.TrimPrefix(strings.TrimSpace(line), "export ")
			if strings.HasPrefix(strings.TrimSpace(line), "GEMINI_TELEMETRY_") {
				return true, true, nil
			}
		}
	}
	return found, false, nil
}

func geminiSettingsPaths(cwd string) ([]string, error) {
	system := os.Getenv("GEMINI_CLI_SYSTEM_SETTINGS_PATH")
	if system == "" {
		system = "/etc/gemini-cli/settings.json"
		if runtime.GOOS == "darwin" {
			system = "/Library/Application Support/GeminiCli/settings.json"
		}
	}
	defaults := os.Getenv("GEMINI_CLI_SYSTEM_DEFAULTS_PATH")
	if defaults == "" {
		defaults = filepath.Join(filepath.Dir(system), "system-defaults.json")
	}
	home := os.Getenv("GEMINI_CLI_HOME")
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return nil, err
		}
	}
	return []string{system, defaults, filepath.Join(home, ".gemini", "settings.json"), filepath.Join(cwd, ".gemini", "settings.json")}, nil
}

// stripJSONComments blanks the // and /* */ comments gemini allows in its
// settings, leaving strings and line numbers as they were.
func stripJSONComments(data []byte) []byte {
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
		case out[i] == '/' && i+1 < len(out) && out[i+1] == '/':
			for ; i < len(out) && out[i] != '\n'; i++ {
				out[i] = ' '
			}
		case out[i] == '/' && i+1 < len(out) && out[i+1] == '*':
			end := i + 2
			for end+1 < len(out) && (out[end] != '*' || out[end+1] != '/') {
				end++
			}
			for stop := min(end+2, len(out)); i < stop; i++ {
				if out[i] != '\n' {
					out[i] = ' '
				}
			}
			i--
		}
	}
	return out
}
