package agentsession

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Env holds the variables an agent launches with over the manager's own.
type Env map[string]string

var managerEnv Env

func (env Env) get(key string) string {
	if value, ok := env[key]; ok {
		return value
	}
	return os.Getenv(key)
}

func (env Env) environ() []string {
	if len(env) == 0 {
		return nil
	}
	environ := os.Environ()
	for key, value := range env {
		environ = append(environ, key+"="+value)
	}
	return environ
}

// ErrNoStore reports a tool whose saved conversations the manager cannot read.
var ErrNoStore = errors.New("tool keeps no conversations the manager reads")

// Saved reports whether a tool launched under env saved conversation id in cwd.
func Saved(sessionStore, sessionReport, cwd, id string, env Env) (bool, error) {
	if sessionStore != "" {
		snapshot, ok := snapshotUnder(sessionStore, cwd, env)
		if !ok {
			return false, fmt.Errorf("cannot read the conversations %s saved", sessionStore)
		}
		_, saved := snapshot[id]
		return saved, nil
	}
	var path string
	var err error
	switch sessionReport {
	case "claude":
		path, err = claudeTranscriptPath(env, cwd, id)
	case "pi":
		path, err = piTranscriptPath(piRoot(env), cwd, id)
	case "grok":
		return grokSaved(grokHome(env), cwd, id)
	default:
		return false, ErrNoStore
	}
	if err == nil {
		_, err = os.Stat(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func grokHome(env Env) string {
	if home := env.get("GROK_HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grok")
}

// grokSaved matches the cwd summary.json records, not its folder's encoding.
func grokSaved(home, cwd, id string) (bool, error) {
	if home == "" {
		return false, nil
	}
	summaries, err := filepath.Glob(filepath.Join(home, "sessions", "*", id, "summary.json"))
	if err != nil {
		return false, err
	}
	wantCwd := resolvePath(cwd)
	for _, path := range summaries {
		data, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		var summary struct {
			Info struct {
				ID  string `json:"id"`
				Cwd string `json:"cwd"`
			} `json:"info"`
		}
		if json.Unmarshal(data, &summary) != nil {
			continue
		}
		if summary.Info.ID == id && resolvePath(summary.Info.Cwd) == wantCwd {
			return true, nil
		}
	}
	return false, nil
}
