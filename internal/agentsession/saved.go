package agentsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Env holds the variables an agent launches with over the manager's own.
type Env map[string]string

var managerEnv Env

func (env Env) get(key string) string {
	for name, value := range env {
		if name == key || runtime.GOOS == "windows" && strings.EqualFold(name, key) {
			return value
		}
	}
	return os.Getenv(key)
}

func (env Env) homeDir() (string, error) {
	variable := "HOME"
	if runtime.GOOS == "windows" {
		variable = "USERPROFILE"
	}
	if home := env.get(variable); home != "" {
		return home, nil
	}
	return os.UserHomeDir()
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

// command finds name on the PATH the launch sets, not the manager's own.
func (env Env) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env.environ()
	if len(env) == 0 {
		return cmd
	}
	for _, dir := range filepath.SplitList(env.get("PATH")) {
		if path, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			cmd.Path, cmd.Err = path, nil
			return cmd
		}
	}
	cmd.Err = &exec.Error{Name: name, Err: exec.ErrNotFound}
	return cmd
}

// ErrNoStore reports a tool whose saved conversations the manager cannot read.
var ErrNoStore = errors.New("tool keeps no conversations the manager reads")

// Launch is where and how a tool starts: its directory, the variables it
// gets and the profile it runs under.
type Launch struct {
	Cwd     string
	Env     Env
	Profile string
}

// Saved reports whether a tool started as launch saved conversation id there.
func Saved(sessionStore, sessionReport, id string, launch Launch) (bool, error) {
	if sessionStore != "" {
		snapshot, ok := snapshotUnder(sessionStore, launch.Cwd, launch.Profile, launch.Env)
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
		path, err = claudeTranscriptPath(launch.Env, launch.Cwd, id)
	case "pi":
		path, err = piTranscriptPath(piRoot(launch.Env), launch.Cwd, id)
	case "grok":
		return grokSaved(grokHome(launch.Env), launch.Cwd, id)
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
	home, err := env.homeDir()
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
