package tmux

import (
	"os"
	"sort"
)

// afterCreateThemeLoad runs between Create loading the pane theme and writing
// it, while Create holds the push lock. Nil in production; a test sets it to
// drive a push against the held lock and prove the write ordering.
var afterCreateThemeLoad func()

func (d *Driver) Create(id, cwd, command string, env map[string]string, width, height int) error {
	name := sessionName(id)
	// Hold the push lock across loading the theme and the command that
	// writes it, so a concurrent PushPaneTheme cannot land a newer theme
	// between the load and the write and be clobbered by this stale one.
	d.paneThemePush.Lock()
	theme := d.paneTheme.Load()
	var colorFgBg string
	if theme != nil {
		colorFgBg = theme.ColorFgBg
	}
	if afterCreateThemeLoad != nil {
		afterCreateThemeLoad()
	}
	// Launch through a script. Typing the full line with send-keys
	// truncates around 1024 bytes, which breaks long first prompts
	// mid-path. A script has no practical length limit, and handing the
	// pane to the user's shell afterwards matches "type into a shell"
	// (pane stays up).
	var scriptPath string
	if command != "" {
		var err error
		scriptPath, err = writeLaunchScript(id, env, command, colorFgBg)
		if err != nil {
			d.paneThemePush.Unlock()
			return err
		}
	}
	runErr := d.startSession(id, cwd, width, height, scriptPath, theme)
	d.paneThemePush.Unlock()
	if runErr != nil {
		if scriptPath != "" {
			os.Remove(scriptPath)
		}
		return runErr
	}
	if err := d.installSessionUX(name); err != nil {
		_ = d.Kill(id)
		return err
	}
	return nil
}

// relaunchHint lands in the pane the moment the agent exits, which is where
// the user is looking when they wonder how to get it back.
const relaunchHint = "agent-manager: agent exited - press v in Agent Manager to relaunch it here."

func sortedKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
