package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	tea "github.com/charmbracelet/bubbletea"
)

type paneKeyMode string

const (
	// extendedKeys is a pane that asked tmux for extended keys (Ext 2).
	extendedKeys paneKeyMode = "extended"
	// legacyKeys is a pane that never asked (VT10x).
	legacyKeys paneKeyMode = "legacy"
)

// builtinKeyModes is the key mode each built-in CLI's pane was seen in,
// read from #{pane_key_mode} under tmux 3.5a with extended-keys on. A CLI
// nobody has probed is listed as legacy, so its row holds the outcome for a
// pane that never asks: the key is dropped, never turned into Enter.
var builtinKeyModes = map[string]struct {
	mode     paneKeyMode
	evidence string
}{
	"claude":       {extendedKeys, "Ext 2, Claude Code 2.1.319"},
	"codex":        {extendedKeys, "Ext 2 on its login screen"},
	"gemini":       {legacyKeys, "VT10x on its trust and auth screens, Gemini CLI 0.67.0; prompt not reached"},
	"opencode":     {legacyKeys, "not verified: crashed on start in the probe"},
	"cursor":       {legacyKeys, "not verified: stopped at login"},
	"pi":           {legacyKeys, "not verified: not installed"},
	"hermes":       {legacyKeys, "not verified: not installed"},
	"grok":         {legacyKeys, "not verified: not installed"},
	"antigravity":  {legacyKeys, "not verified: not installed"},
	"command-code": {legacyKeys, "not verified: not installed"},
	"muse":         {legacyKeys, "not verified: not installed"},
	"omp":          {legacyKeys, "not verified: not installed"},
	"kiro":         {legacyKeys, "not verified: not installed"},
}

// Shift+Enter and Ctrl+Enter reach a focused CLI as a new line when its pane
// asked for extended keys, and are dropped in any other pane, where tmux would
// send them as Enter and submit the prompt. Each built-in CLI is accounted for
// by the key mode its pane was seen in.
func TestFocusedModifiedEnterFollowsEachToolsKeyMode(t *testing.T) {
	shipped, err := config.Default()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	for name, tool := range shipped.Tools {
		if _, ok := builtinKeyModes[name]; !ok && !tool.Shell {
			t.Errorf("built-in tool %q has no key mode in builtinKeyModes; read its pane's #{pane_key_mode} and add it", name)
		}
	}

	// tmux before 3.5 cannot report a pane's key mode, so the key is dropped
	// in every pane there.
	readable := tmuxAtLeast(t, 3, 5)
	for _, mode := range []paneKeyMode{extendedKeys, legacyKeys} {
		t.Run(string(mode), func(t *testing.T) {
			want := "typed"
			if mode == extendedKeys && readable {
				want = "\x1b[13;2u\x1b[13;5utyped"
			}
			if got := focusedKeysRead(t, mode); got != want {
				t.Errorf("a %s pane read %q, want %q", mode, got, want)
			}
		})
	}
}

// focusedKeysRead focuses a pane in mode, sends Shift+Enter, Ctrl+Enter, the
// text "typed" and Enter, and returns the line the pane read.
func focusedKeysRead(t *testing.T, mode paneKeyMode) string {
	t.Helper()
	m := buildModel(t)
	request := ""
	if mode == extendedKeys {
		request = `printf "\033[>4;2m"; `
	}
	toolName := string(mode) + "-keys"
	dir := t.TempDir()
	keys := filepath.Join(dir, "keys")
	m.services.cfg.Tools[toolName] = config.Tool{Command: `sh -c '` + request + `: > ready; exec head -n 1 > keys'`, DefaultStatus: status.Idle}
	createSessionOn(t, m, toolName, toolName, dir)
	m.selectSessionRow(t, toolName)
	sessID := railSelectedSession(m).ID
	// A ready file, not waitForPaneChild: on Windows the pane's child stays
	// sh while head runs beneath it.
	ready := filepath.Join(dir, "ready")
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the pane never started its key reader")
		}
	}
	if mode == extendedKeys && tmuxAtLeast(t, 3, 5) {
		deadline := time.Now().Add(10 * time.Second)
		var keyMode string
		for time.Now().Before(deadline) {
			out, err := tmuxCmd(t, "display-message", "-p", "-t", tmux.PaneTarget(sessID), "#{pane_key_mode}").Output()
			if err != nil {
				t.Fatalf("read pane key mode: %v", err)
			}
			keyMode = strings.TrimSpace(string(out))
			if strings.HasPrefix(keyMode, "Ext ") {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !strings.HasPrefix(keyMode, "Ext ") {
			t.Fatalf("pane key mode = %q, want Ext", keyMode)
		}
	}
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("after enter, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	for _, msg := range []tea.Msg{
		uifocus.ExtendedKeyMsg{Name: "S-Enter"},
		uifocus.ExtendedKeyMsg{Name: "C-Enter"},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("typed")},
		tea.KeyMsg{Type: tea.KeyEnter},
	} {
		updated, _ := m.Update(msg)
		*m = *updated.(*Model)
		m.drainEffects(t)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(keys); err == nil && len(data) > 0 {
			return string(data[:len(data)-1])
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the pane never wrote %s", keys)
	return ""
}

func tmuxAtLeast(t *testing.T, major, minor int) bool {
	t.Helper()
	version, err := exec.Command(tmux.Binary, tmux.SocketArgs(testSocket, "-V")...).Output()
	if err != nil {
		t.Fatalf("tmux -V: %v", err)
	}
	match := regexp.MustCompile(`(\d+)\.(\d+)`).FindStringSubmatch(string(version))
	if match == nil {
		t.Fatalf("tmux -V = %q, want a version", version)
	}
	gotMajor, _ := strconv.Atoi(match[1])
	gotMinor, _ := strconv.Atoi(match[2])
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}
