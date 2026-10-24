package persist

import (
	"errors"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
)

// scriptReader answers a fixed map and can fail one key.
type scriptReader struct {
	values map[string]string
	failOn string
}

func (r *scriptReader) Setting(key string) (string, error) {
	if key == r.failOn {
		return "", errors.New("scripted read failure")
	}
	return r.values[key], nil
}

// New caches the dialog keys and the extra keys the caller names.
func TestNewCachesTheDialogAndExtraKeys(t *testing.T) {
	reader := &scriptWriter{values: map[string]string{
		uisettings.ListDensitySetting: "comfortable",
		"choice.claude":               `{"Model":"opus"}`,
	}}
	state := New(reader, []string{"choice.claude"})
	if state.Cache.Value(uisettings.ListDensitySetting) != "comfortable" {
		t.Fatal("the dialog key is missing from the cache")
	}
	if state.Cache.Value("choice.claude") == "" {
		t.Fatal("the extra key is missing from the cache")
	}
}

// A load reports how many reads failed, naming the first.
func TestLoadCountsReadFailures(t *testing.T) {
	reader := &scriptReader{
		values: map[string]string{},
		failOn: uisettings.ListDensitySetting,
	}
	_, err := Load(reader)
	if err == nil {
		t.Fatal("expected the failed read to surface")
	}
	if got := err.Error(); got == "" {
		t.Fatal("the load error carries no reason")
	}
}

// A stale generation may not rebuild its surface, and neither may a surface
// whose save is still on the lane.
func TestFreshFencesOnGenerationAndPending(t *testing.T) {
	state := State{Gen: 3}
	if !state.Fresh(3) {
		t.Fatal("the current generation is not fresh")
	}
	if state.Fresh(2) {
		t.Fatal("an earlier generation reported fresh")
	}
	state.Pending = 1
	if state.Fresh(3) {
		t.Fatal("a pending save reported fresh")
	}
}

// FromCache builds the dialog the cached values describe, including the
// hidden set narrowed to the configured tools.
func TestFromCacheBuildsTheDialog(t *testing.T) {
	cfg := config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "claude"},
		"codex":  {Command: "codex"},
	}}
	reader := &scriptWriter{values: map[string]string{
		uisettings.HiddenToolsSetting: "codex",
		uisettings.ListDensitySetting: "comfortable",
	}}
	state := New(reader, nil)
	dialog := state.FromCache(cfg, "nord")
	if !dialog.ComfortableRows {
		t.Fatal("the cached density did not reach the dialog")
	}
	if !dialog.CLIHidden["codex"] {
		t.Fatal("the cached hidden CLI did not reach the dialog")
	}
}

// The tool selection falls back to the cached default when the caller
// prefers nothing.
func TestToolSelectionFallsBackToTheCachedDefault(t *testing.T) {
	cfg := config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "claude"},
		"codex":  {Command: "codex"},
	}}
	reader := &scriptWriter{values: map[string]string{uisettings.DefaultToolSetting: "codex"}}
	state := New(reader, nil)
	names, index := state.ToolSelection(cfg, nil, "")
	if len(names) == 0 {
		t.Fatal("no tools came back")
	}
	if names[index] != "codex" {
		t.Fatalf("selection landed on %q, want the cached default codex", names[index])
	}
}
