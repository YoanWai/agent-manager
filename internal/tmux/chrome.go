package tmux

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

// PaneTheme is the background agent panes are rendered on. The manager
// knows that color — it paints every capture on it and repaints the
// terminal to it for a full-screen attach — but an agent inside a pane
// cannot discover it: these sessions run on a server whose only client is
// in control mode, so there is no terminal to answer an OSC 11 background
// query, and the environment carries no COLORFGBG either. Declaring both
// on the server hands an auto-detecting agent the answer the manager
// already renders, instead of leaving it to guess.
type PaneTheme struct {
	Background string // "#rrggbb"; tmux answers pane OSC 11 queries with it
	ColorFgBg  string // "fg;bg" color indexes for agents reading COLORFGBG
}

// paneThemeArgs is the option pair as a tmux command list. window-style and
// the environment are both server-global: every managed session lives on
// this socket and wants the same answer, and a global option also reaches
// windows a user opens inside a session later.
func paneThemeArgs(t PaneTheme) []string {
	return []string{
		"set-option", "-g", "window-style", "bg=" + t.Background, ";",
		"set-environment", "-g", "COLORFGBG", t.ColorFgBg,
	}
}

// PublishPaneTheme records the pane colors so Create hands them to new
// sessions. It only stores the value; PushPaneTheme sends it to a running
// server. Recording is synchronous so a session created right after a theme
// change still opens on the chosen background.
func (d *Driver) PublishPaneTheme(t PaneTheme) {
	d.paneTheme.Store(&t)
}

// PushPaneTheme sends the recorded pane theme to a running server. It always
// pushes the latest published value under a lock, so concurrent pushes are
// latest-wins: whichever runs last writes the current theme rather than an
// older one it was spawned for. A server with no sessions exits immediately,
// so there is nothing to push to before the first session exists; Create
// re-applies the recorded theme in the same command list as its new-session,
// which is also what keeps the option set before the agent process can query
// it.
func (d *Driver) PushPaneTheme() error {
	d.paneThemePush.Lock()
	defer d.paneThemePush.Unlock()
	theme := d.paneTheme.Load()
	if theme == nil {
		return nil
	}
	out, err := exec.Command(d.bin, d.args(paneThemeArgs(*theme)...)...).CombinedOutput()
	if err != nil {
		if noServer(string(out)) {
			return nil
		}
		return fmt.Errorf("tmux set pane theme: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *Driver) installSessionUX(name string) error {
	if err := d.EnsureBindings(); err != nil {
		return err
	}
	if err := d.styleStatusBar(name); err != nil {
		return err
	}
	_, err := d.run("set-option", "-t", name, "status-left", "")
	return err
}

// Tmux answers an un-overridden session option with an empty string rather than the global option.
func (d *Driver) resolvedOption(name, option string) (string, error) {
	value, err := d.run("show-options", "-t", name, "-v", option)
	if err != nil {
		return "", err
	}
	if value = strings.TrimSpace(value); value != "" {
		return value, nil
	}
	global, err := d.run("show-options", "-g", "-v", option)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(global), nil
}

// styleStatusBar sets a session's status bar chrome, leaving status-left (the
// name label) untouched so re-styling a live session keeps its label.
func (d *Driver) styleStatusBar(name string) error {
	primary, err := d.resolvedOption(name, "prefix")
	if err != nil {
		return err
	}
	secondary, err := d.resolvedOption(name, "prefix2")
	if err != nil {
		return err
	}
	options := [][]string{
		{"set-option", "-t", name, "status", "on"},
		// The default status-right-length of 40 truncates the hints, so widen it
		// to fit the whole footer, including the configured-prefix fallback.
		{"set-option", "-t", name, "status-right-length", "100"},
		{"set-option", "-t", name, "status-right", attachStatusRight(primary, secondary, d.currentSessionKeys())},
		{"set-option", "-t", name, "status-style", "bg=colour236,fg=colour249"},
		// hide the "0:windowname*" window list; it reads as noise here
		{"set-option", "-t", name, "window-status-format", ""},
		{"set-option", "-t", name, "window-status-current-format", ""},
		// mouse on so tmux handles scrollback per-session instead of the
		// terminal emulator, whose buffer carries content from prior attaches.
		{"set-option", "-t", name, "mouse", "on"},
	}
	_, err = d.run(commandList(options...)...)
	return err
}

// attachStatusRight is the session footer: the keys the manager keeps,
// and every way back. A detach key the inner prefix shadows is left off,
// since the prefix takes it first, and the prefix itself follows with d.
func attachStatusRight(primary, secondary string, keys keybind.Table) string {
	parts := []string{"agent-manager"}
	if label := titledLabel(keys.Binding(keybind.Review)); label != "" {
		parts = append(parts, label+" = review")
	}
	if label := titledLabel(keys.Binding(keybind.Editor)); label != "" {
		parts = append(parts, label+" = editor")
	}
	var exits []string
	for _, key := range keys.Binding(keybind.Detach).Keys() {
		if key.Tmux() != primary && key.Tmux() != secondary {
			exits = append(exits, titled(key))
		}
	}
	for _, candidate := range []string{primary, secondary} {
		if candidate != "" && candidate != "None" {
			exits = append(exits, candidate+" d")
			break
		}
	}
	parts = append(parts, strings.Join(exits, " / ")+" = back")
	return " " + strings.Join(parts, " · ") + " "
}

func titledLabel(binding keybind.Binding) string {
	names := make([]string, 0, len(binding.Keys()))
	for _, key := range binding.Keys() {
		names = append(names, titled(key))
	}
	return strings.Join(names, " / ")
}

func titled(key keybind.Key) string {
	name := key.Tea()
	return strings.ToUpper(name[:1]) + name[1:]
}

// ownedBindingTest is the session-name check every root binding the
// manager installs carries, so that on the next run its own bindings can
// be told from the ones the user's tmux.conf put on this server.
const ownedBindingTest = "#{m:" + prefix + "*,#{session_name}}"

// EnsureBindings installs the server-wide setup every managed session
// relies on: the in-session key bindings, and the pane numbering
// PaneTarget addresses. A tmux config that starts panes at 1 is the
// dangerous case for the numbering, because tmux answers a target whose
// index does not exist with the active pane rather than an error, so
// every capture and keystroke would silently follow the focused pane.
//
// The bindings an earlier run installed come off first, whatever keys
// that run was configured with: a key the user moved or turned off would
// otherwise stay bound until the server restarts.
func (d *Driver) EnsureBindings() error {
	stale, err := d.ownedRootBindings()
	if err != nil {
		return err
	}
	keys := d.currentSessionKeys()
	request := func(name string) string {
		return "set-option -g " + requestOption + " " + name + " ; detach-client"
	}
	commands := [][]string{
		{"set-window-option", "-g", "pane-base-index", "0"},
	}
	for _, key := range stale {
		commands = append(commands, []string{"unbind-key", "-T", "root", key})
	}
	for _, key := range keys.Binding(keybind.Detach).Keys() {
		commands = append(commands, rootBinding(key, "detach-client"))
	}
	for _, key := range keys.Binding(keybind.Review).Keys() {
		commands = append(commands, rootBinding(key, request(RequestReview)))
	}
	for _, key := range keys.Binding(keybind.Editor).Keys() {
		commands = append(commands, rootBinding(key, request(RequestEditor)))
	}
	// Restore the standard fallback when the prefix shadows a direct binding.
	commands = append(commands, []string{"bind-key", "-T", "prefix", "d", "detach-client"})
	_, err = d.run(commandList(commands...)...)
	return err
}

// rootBinding binds a key inside managed sessions only; anywhere else on
// the server the key goes through to the pane as itself. That branch is a
// command string tmux parses, so a backslash in the key name is doubled.
func rootBinding(key keybind.Key, action string) []string {
	passThrough := "send-keys " + strings.ReplaceAll(key.Tmux(), `\`, `\\`)
	return []string{"bind-key", "-n", key.Tmux(), "if-shell", "-F", ownedBindingTest, action, passThrough}
}

// ownedRootBindings lists the root-table keys carrying the manager's own
// session test. list-keys prints a key the way its parser reads it back,
// so a backslash comes doubled and is undone here for unbind-key, which
// takes the name as is.
func (d *Driver) ownedRootBindings() ([]string, error) {
	out, err := d.run("list-keys", "-T", "root")
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "bind-key" || fields[1] != "-T" || fields[2] != "root" {
			continue
		}
		if !strings.Contains(line, ownedBindingTest) {
			continue
		}
		keys = append(keys, strings.ReplaceAll(fields[3], `\\`, `\`))
	}
	return keys, nil
}

// RefreshChrome re-applies the status bar chrome to a live session so a
// session created before a manager update picks up the current footer,
// without disturbing its name label.
func (d *Driver) RefreshChrome(id string) error {
	return d.styleStatusBar(sessionName(id))
}

// SetLabel puts the session's name and group path in the status bar's
// left side, replacing the hidden window list.
func (d *Driver) SetLabel(id, label string) error {
	name := sessionName(id)
	if _, err := d.run("set-option", "-t", name, "status-left-length", "80"); err != nil {
		return err
	}
	_, err := d.run("set-option", "-t", name, "status-left", " "+sanitizeFormat(label)+" ")
	return err
}

// sanitizeFormat neutralizes tmux format expansion in user-supplied text.
// Status bars expand #(shell command) and friends, so a session named
// "#(cmd)" would otherwise execute when the bar renders. tmux escapes
// a literal # as ##. Control characters are dropped.
func sanitizeFormat(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.ReplaceAll(s, "#", "##")
}
