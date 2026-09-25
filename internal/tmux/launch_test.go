package tmux

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// Create used to type the launch line with send-keys, which silently
// truncates around 1024 bytes and left long first prompts as a broken
// shell command. paste-buffer must deliver the full line.
func TestCreateDeliversLongCommand(t *testing.T) {
	driver := requireTmux(t)
	id := "long" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := "/tmp/am-long-" + id
	t.Cleanup(func() { os.Remove(marker) })

	payload := strings.Repeat("x", 1500)
	command := "printf '%s' '" + payload + "' > " + marker
	if len(command) < 1024 {
		t.Fatalf("test command must exceed the old 1024-byte send-keys limit, got %d", len(command))
	}
	if err := driver.Create(id, "/tmp", command, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(marker)
		if err == nil && string(data) == payload {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	pane, _ := driver.CapturePane(id)
	got, _ := os.ReadFile(marker)
	t.Fatalf("long launch command truncated or failed; wrote %d bytes, want %d; pane:\n%s", len(got), len(payload), pane)
}

// An agent that auto-detects its palette asks the terminal for its
// background with OSC 11. Nothing answers that on this server — the only
// client is in control mode and has no tty — unless the pane carries an
// explicit background of its own, which is what the pane theme sets.
func TestCreateAnswersBackgroundQuery(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })
	driver.PublishPaneTheme(PaneTheme{Background: "#1e1e2e", ColorFgBg: "15;0"})

	id := "osc" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	reply := t.TempDir() + "/reply"
	// tmux delivers the answer on the pane's input, so the query and the
	// read both happen inside the pane. Raw mode keeps the line discipline
	// from holding a reply that ends in ST rather than a newline.
	command := "stty raw; printf '\\033]11;?\\033\\\\'; cat > " + reply
	if err := driver.Create(id, "/tmp", command, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	got := waitForFile(t, driver, id, reply)
	if want := "]11;rgb:1e1e/1e1e/2e2e"; !strings.Contains(got, want) {
		t.Fatalf("OSC 11 reply = %q, want one carrying %q", got, want)
	}
}

// COLORFGBG is the fallback for agents that read the environment instead of
// querying, and nothing in a pane's environment carries it otherwise.
func TestCreateExportsColorFgBg(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })
	driver.PublishPaneTheme(PaneTheme{Background: "#1e1e2e", ColorFgBg: "15;0"})

	id := "fgbg" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := t.TempDir() + "/env"
	if err := driver.Create(id, "/tmp", "printenv COLORFGBG > "+marker, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if got := strings.TrimSpace(waitForFile(t, driver, id, marker)); got != "15;0" {
		t.Fatalf("COLORFGBG in pane = %q, want %q", got, "15;0")
	}
}

// The session environment has to outlive the launch command. Quitting the
// agent drops the pane onto the shell the script execs, and an agent
// started again from that shell belongs to this managed session only if it
// inherits these values: the rename subcommand and the MCP server both
// identify the session by AGENT_MANAGER_SESSION_ID alone.
func TestCreateExportsSessionEnvIntoTheShell(t *testing.T) {
	driver := requireTmux(t)
	id := "senv" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := t.TempDir() + "/env"
	env := map[string]string{
		"AGENT_MANAGER_SESSION_ID":  "abc123",
		"AGENT_MANAGER_STATUS_FILE": "/tmp/status-abc123",
	}
	// A launch command that returns at once stands in for the user quitting
	// the agent back to the pane's shell.
	if err := driver.Create(id, "/tmp", "printf 'agent ran\\n'", env, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	waitForPane(t, driver, id, relaunchHint)

	// Written whole and moved into place, so the read cannot land between
	// the two values.
	report := `printf '%s %s\n' "$AGENT_MANAGER_SESSION_ID" "$AGENT_MANAGER_STATUS_FILE" > ` +
		marker + `.part && mv ` + marker + `.part ` + marker
	if err := driver.SendKeys(id, report, "Enter"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	got := strings.Fields(waitForFile(t, driver, id, marker))
	want := []string{"abc123", "/tmp/status-abc123"}
	if !slices.Equal(got, want) {
		t.Fatalf("environment left to the shell = %v, want %v", got, want)
	}
}

// The pane is where the user is looking when the agent exits, so the way
// back to a wired agent is named there.
func TestCreateNamesTheWayBackWhenTheAgentExits(t *testing.T) {
	driver := requireTmux(t)
	id := "hint" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "printf 'agent ran\\n'", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	waitForPane(t, driver, id, relaunchHint)
}

func TestExportEnvPrefixesTheCommand(t *testing.T) {
	env := map[string]string{"B": "second", "A": "fir st"}
	want := `export A='fir st'; export B='second'; claude --resume 7`
	if got := ExportEnv(env, "claude --resume 7"); got != want {
		t.Fatalf("ExportEnv = %q, want %q", got, want)
	}
}
