package tmux

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

func (d *Driver) installSessionUX(name string) error {
	if err := d.ensureBindingsIn(strings.TrimPrefix(name, prefix)); err != nil {
		return err
	}
	// A new session carries no pin to take off, so only a chosen prefix needs work here.
	if keys := d.tmuxPrefixKeys(); len(keys) > 0 {
		if err := d.pinPrefix(name, keys); err != nil {
			return err
		}
	}
	if err := d.styleStatusBar(name); err != nil {
		return err
	}
	_, err := d.run("set-option", "-t", name, "status-left", "")
	return err
}

func (d *Driver) tmuxPrefixKeys() []keybind.Key {
	return d.currentSessionKeys().Binding(keybind.TmuxPrefix).Keys()
}

// Set per session, so unsetting hands back the server-wide prefix tmux.conf sets.
func (d *Driver) pinPrefix(name string, keys []keybind.Key) error {
	secondary := "None"
	if len(keys) > 1 {
		secondary = keys[1].Tmux()
	}
	_, err := d.run(commandList(
		[]string{"set-option", "-t", name, "prefix", keys[0].Tmux()},
		[]string{"set-option", "-t", name, "prefix2", secondary},
		[]string{"set-option", "-t", name, pinnedPrefixOption, "on"},
	)...)
	return err
}

func (d *Driver) unpinPrefix(name string) error {
	pinned, err := d.run("show-options", "-q", "-v", "-t", name, pinnedPrefixOption)
	if err != nil {
		return err
	}
	if strings.TrimSpace(pinned) == "" {
		return nil
	}
	_, err = d.run(commandList(
		[]string{"set-option", "-u", "-t", name, "prefix"},
		[]string{"set-option", "-u", "-t", name, "prefix2"},
		[]string{"set-option", "-u", "-t", name, pinnedPrefixOption},
	)...)
	return err
}

// Tmux answers an un-overridden session option with an empty string rather than the global option.
func (d *Driver) resolvedOption(name, option string) (string, error) {
	value, err := d.run("show-options", "-t", name, "-v", option)
	if err != nil {
		return "", err
	}
	if value = keyOption(value); value != "" {
		return value, nil
	}
	global, err := d.runIn(strings.TrimPrefix(name, prefix), "show-options", "-g", "-v", option)
	if err != nil {
		return "", err
	}
	return keyOption(global), nil
}

// keyOption reads a key option as tmux printed it. tmux 3.1 prints a prefix
// set to None as Invalid# and a hex code, which later versions print as None.
func keyOption(printed string) string {
	printed = strings.TrimSpace(printed)
	if strings.HasPrefix(printed, "Invalid#") {
		return "None"
	}
	return printed
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
		{"set-option", "-t", name, "window-status-format", hiddenWindowStatus},
		{"set-option", "-t", name, "window-status-current-format", hiddenWindowStatus},
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
// manager installs on tmux carries, so that on the next run its own
// bindings can be told from the ones the user's tmux.conf put on this
// server. psmux records its keys instead (rootKeysOption).
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
//
// psmux keeps options and bindings per session server, so there the setup
// goes to every managed session's server in turn.
func (d *Driver) EnsureBindings() error {
	return d.eachServer(d.ensureBindingsIn)
}

// ensureBindingsIn installs the setup on the server hosting session id.
func (d *Driver) ensureBindingsIn(id string) error {
	stale, err := d.ownedRootBindings(id)
	if err != nil {
		return err
	}
	keys := d.currentSessionKeys()
	commands := [][]string{
		{"set-window-option", "-g", "pane-base-index", "0"},
	}
	for _, key := range stale {
		commands = append(commands, []string{"unbind-key", "-T", "root", key})
	}
	var bound []string
	bind := func(binding keybind.Binding, action []string) {
		for _, key := range binding.Keys() {
			commands = append(commands, rootBinding(key, action...))
			bound = append(bound, key.Tmux())
		}
	}
	bind(keys.Binding(keybind.Detach), []string{"detach-client"})
	bind(keys.Binding(keybind.Review), d.requestAction(id, RequestReview))
	bind(keys.Binding(keybind.Editor), d.requestAction(id, RequestEditor))
	commands = append(commands, recordRootBindings(bound)...)
	// Restore the standard fallback when the prefix shadows a direct binding.
	commands = append(commands, []string{"bind-key", "-T", "prefix", "d", bindingArg("detach-client")})
	// list-keys starts a server that exits again straight away when it has
	// no sessions, so the list can find none. Nothing needs the setup then:
	// Create runs this again once a session brings the server up.
	if _, err = d.runIn(id, commandList(commands...)...); err != nil && noServer(err.Error()) {
		return nil
	}
	return err
}

// RefreshChrome re-applies the prefix and status bar chrome, keeping the session's name label.
func (d *Driver) RefreshChrome(id string) error {
	name := sessionName(id)
	var err error
	if keys := d.tmuxPrefixKeys(); len(keys) > 0 {
		err = d.pinPrefix(name, keys)
	} else {
		err = d.unpinPrefix(name)
	}
	if err != nil {
		return err
	}
	return d.styleStatusBar(name)
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
