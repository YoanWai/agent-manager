package tmux

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

// resolvedOption's global fallback can fail on its own (no server, a
// stale socket); that error must reach the caller, not just the
// session-scoped read's.
func TestResolvedOptionPropagatesTheGlobalFallbackError(t *testing.T) {
	dir := t.TempDir()
	stub := dir + "/tmux"
	script := "#!/bin/sh\ncase \"$*\" in *'-g -v prefix'*) echo 'no server running' >&2; exit 1;; esac\nexit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}

	if _, err := driver.resolvedOption("x1", "prefix"); err == nil {
		t.Fatal("resolvedOption should propagate the global fallback's error")
	}
}

// With no server up, list-keys still answers from a server that exits
// straight after, and only the command list finds none. That is not an
// error: Create installs the bindings once a session starts the server.
func TestEnsureBindingsIgnoresAMissingServer(t *testing.T) {
	dir := t.TempDir()
	stub := dir + "/tmux"
	script := "#!/bin/sh\ncase \"$*\" in *list-keys*) exit 0;; esac\necho 'no server running on /tmp/agentmgr' >&2; exit 1\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings with no server: %v", err)
	}
}

// Create runs on the UI's update path, and a new session cannot carry a pin,
// so with tmux_prefix off only a refresh looks for one.
func TestOnlyARefreshLooksForAPinnedPrefix(t *testing.T) {
	dir := t.TempDir()
	stub := dir + "/tmux"
	script := "#!/bin/sh\ncase \"$*\" in *" + pinnedPrefixOption + "*) echo 'pin read' >&2; exit 1;; esac\nexit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}

	if err := driver.installSessionUX("am_new"); err != nil {
		t.Fatalf("installing a session with tmux_prefix off should not read the pin: %v", err)
	}
	if err := driver.RefreshChrome("live"); err == nil || !strings.Contains(err.Error(), "pin read") {
		t.Fatalf("a refresh should look for a pin to take off, err = %v", err)
	}
}

func TestSetLabelNeutralizesFormatStrings(t *testing.T) {
	driver := requireTmux(t)
	id := "lbl" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	marker := "/tmp/am-injection-" + id
	if err := driver.SetLabel(id, "evil #(touch "+marker+") name"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	rendered, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-left}").CombinedOutput()
	if err != nil {
		t.Fatalf("display-message: %v", err)
	}
	if !strings.Contains(string(rendered), "#(touch") {
		t.Fatalf("format string should render literally, got %q", rendered)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		os.Remove(marker)
		t.Fatal("injection executed: marker file was created")
	}
}

// The editor key moved off C-o, which the agents running inside a session
// bind themselves. A server that predates the move still carries the old
// binding, so EnsureBindings has to drop it as well as install F3.
func TestEnsureBindingsMovesTheEditorKeyToF3(t *testing.T) {
	driver := requireTmux(t)
	stale := []string{"bind-key", "-n", "C-o", "if-shell", "-F", ownedBindingTest,
		"set-option -g " + requestOption + " " + RequestEditor + " ; detach-client", "send-keys C-o"}
	if out, err := tmuxCmd(stale...).CombinedOutput(); err != nil {
		t.Fatalf("seed the old binding: %v: %s", err, out)
	}

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}

	bound, err := tmuxCmd("list-keys", "-T", "root").CombinedOutput()
	if err != nil {
		t.Fatalf("list root keys: %v: %s", err, bound)
	}
	editor := false
	for _, line := range strings.Split(string(bound), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if fields[3] == "C-o" {
			t.Fatalf("C-o should be unbound, got %q", line)
		}
		if fields[3] == "F3" && strings.Contains(line, RequestEditor) {
			editor = true
		}
	}
	if !editor {
		t.Fatalf("F3 should request the editor, got %q", bound)
	}
}

func TestEnsureBindingsRestoresPrefixDetach(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() {
		if out, err := tmuxCmd("bind-key", "-T", "prefix", "d", "detach-client").CombinedOutput(); err != nil {
			t.Errorf("restore prefix d: %v: %s", err, out)
		}
	})
	if out, err := tmuxCmd("unbind-key", "-T", "prefix", "d").CombinedOutput(); err != nil {
		t.Fatalf("unbind prefix d: %v: %s", err, out)
	}

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}
	bound, err := tmuxCmd("list-keys", "-T", "prefix").CombinedOutput()
	if err != nil {
		t.Fatalf("list prefix d: %v: %s", err, bound)
	}
	found := false
	for _, line := range strings.Split(string(bound), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "bind-key" && fields[1] == "-T" && fields[2] == "prefix" && fields[3] == "d" && fields[4] == "detach-client" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("prefix d should detach, got %q", bound)
	}
}

func TestRefreshChromeKeepsLabelAndAddsSessionHints(t *testing.T) {
	driver := requireTmux(t)
	id := "chr" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if err := driver.SetLabel(id, "my-session"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix", `C-\`).CombinedOutput(); err != nil {
		t.Fatalf("set prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome: %v", err)
	}

	right, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right: %v", err)
	}
	if !strings.Contains(string(right), "Ctrl+r = review") {
		t.Fatalf("footer should advertise review, got %q", right)
	}
	if !strings.Contains(string(right), `C-\ d = back`) {
		t.Fatalf("footer should advertise the configured-prefix escape, got %q", right)
	}
	if !strings.Contains(string(right), `Ctrl+q / C-\ d = back`) {
		t.Fatalf("footer should advertise the available direct escape, got %q", right)
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix", "C-q").CombinedOutput(); err != nil {
		t.Fatalf("set conflicting prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome with conflicting prefix: %v", err)
	}
	right, err = tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("conflicting status-right: %v", err)
	}
	if strings.Contains(string(right), "Ctrl+q /") {
		t.Fatalf("footer should hide a direct shortcut claimed by the prefix, got %q", right)
	}
	if !strings.Contains(string(right), "C-q d = back") {
		t.Fatalf("footer should retain the prefix escape, got %q", right)
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix", `C-\`).CombinedOutput(); err != nil {
		t.Fatalf("restore primary prefix: %v: %s", err, out)
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix2", "C-q").CombinedOutput(); err != nil {
		t.Fatalf("set conflicting secondary prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome with conflicting secondary prefix: %v", err)
	}
	right, err = tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("secondary-prefix status-right: %v", err)
	}
	if strings.Contains(string(right), "Ctrl+q /") {
		t.Fatalf("footer should hide a direct shortcut claimed by prefix2, got %q", right)
	}
	if !strings.Contains(string(right), `C-\ d = back`) {
		t.Fatalf("footer should retain the primary-prefix escape, got %q", right)
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix", "None").CombinedOutput(); err != nil {
		t.Fatalf("disable prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome without prefix: %v", err)
	}
	right, err = tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("prefix-free status-right: %v", err)
	}
	if strings.Contains(string(right), "Ctrl+q") {
		t.Fatalf("footer should hide a direct shortcut claimed by prefix2, got %q", right)
	}
	if !strings.Contains(string(right), "C-q d = back") {
		t.Fatalf("footer should show prefix2 when the primary prefix is disabled, got %q", right)
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix2", "None").CombinedOutput(); err != nil {
		t.Fatalf("disable secondary prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome without either prefix: %v", err)
	}
	right, err = tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("prefix-free status-right: %v", err)
	}
	if strings.Contains(string(right), "None d") || !strings.Contains(string(right), `Ctrl+q / Ctrl+\ = back`) {
		t.Fatalf("footer should retain only the direct escapes without prefixes, got %q", right)
	}
	length, err := tmuxCmd("show-option", "-t", "am_"+id, "-v", "status-right-length").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right-length: %v", err)
	}
	if strings.TrimSpace(string(length)) != "100" {
		t.Fatalf("status-right-length should fit the footer, got %q", length)
	}
	left, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-left}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-left: %v", err)
	}
	if !strings.Contains(string(left), "my-session") {
		t.Fatalf("re-styling should keep the name label, got %q", left)
	}
}

// A tmux.conf almost always sets the prefix with "set -g", which
// TestRefreshChromeKeepsLabelAndAddsSessionHints never exercises (it
// always overrides "-t <session>" directly).
func TestRefreshChromeResolvesAGloballySetPrefix(t *testing.T) {
	driver := requireTmux(t)
	original, err := tmuxCmd("show-options", "-g", "-v", "prefix").CombinedOutput()
	if err != nil {
		t.Fatalf("show-options prefix: %v: %s", err, original)
	}
	snapshot := strings.TrimSpace(string(original))
	t.Cleanup(func() {
		if out, err := tmuxCmd("set-option", "-g", "prefix", snapshot).CombinedOutput(); err != nil {
			t.Errorf("restore prefix: %v: %s", err, out)
		}
	})

	id := "globalprefix" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if out, err := tmuxCmd("set-option", "-g", "prefix", "C-q").CombinedOutput(); err != nil {
		t.Fatalf("set global prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome: %v", err)
	}

	right, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right: %v", err)
	}
	if strings.Contains(string(right), "Ctrl+q /") {
		t.Fatalf("footer should hide the default detach key claimed by a globally-set prefix, got %q", right)
	}
	if !strings.Contains(string(right), "C-q d = back") {
		t.Fatalf("footer should advertise the prefix escape for a globally-set prefix, got %q", right)
	}
}

// tmux.conf's prefix beats every binding, so tmux_prefix pins another one to free that key.
func TestTmuxPrefixFreesTheServersPrefixForASessionKey(t *testing.T) {
	driver := requireTmux(t)
	restoreDefaultKeys(t, driver)
	original, err := tmuxCmd("show-options", "-g", "-v", "prefix").CombinedOutput()
	if err != nil {
		t.Fatalf("show-options prefix: %v: %s", err, original)
	}
	t.Cleanup(func() {
		if out, err := tmuxCmd("set-option", "-g", "prefix", strings.TrimSpace(string(original))).CombinedOutput(); err != nil {
			t.Errorf("restore prefix: %v: %s", err, out)
		}
	})
	if out, err := tmuxCmd("set-option", "-g", "prefix", "C-s").CombinedOutput(); err != nil {
		t.Fatalf("set the tmux.conf prefix: %v: %s", err, out)
	}
	detachOnCtrlS := keybind.DefaultSession().With(keybind.Detach, bindingOf(t, "ctrl+s"))
	driver.SetSessionKeys(detachOnCtrlS.With(keybind.TmuxPrefix, bindingOf(t, "ctrl+b")))
	id := "tmuxprefix" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	sessionOption := func(option string) string {
		t.Helper()
		out, err := tmuxCmd("show-options", "-q", "-v", "-t", "am_"+id, option).CombinedOutput()
		if err != nil {
			t.Fatalf("show-options %s: %v: %s", option, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	footer := func() string {
		t.Helper()
		right, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
		if err != nil {
			t.Fatalf("status-right: %v: %s", err, right)
		}
		return string(right)
	}
	if got := sessionOption("prefix") + " " + sessionOption("prefix2"); got != "C-b None" {
		t.Fatalf("the session should answer to the pinned prefix alone, got %q", got)
	}
	if right := footer(); !strings.Contains(right, "Ctrl+s / C-b d = back") {
		t.Fatalf("ctrl+s should be free to detach, got %q", right)
	}

	driver.SetSessionKeys(detachOnCtrlS)
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome without tmux_prefix: %v", err)
	}
	if got := sessionOption("prefix"); got != "" {
		t.Fatalf("clearing tmux_prefix should hand back the server's prefix, the session still sets %q", got)
	}
	if right := footer(); strings.Contains(right, "Ctrl+s /") || !strings.Contains(right, "C-s d = back") {
		t.Fatalf("the server's prefix should shadow ctrl+s again, got %q", right)
	}

	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix", "C-a").CombinedOutput(); err != nil {
		t.Fatalf("set a session prefix by hand: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome over a hand-set prefix: %v", err)
	}
	if got := sessionOption("prefix"); got != "C-a" {
		t.Fatalf("a prefix the manager never set should stay, got %q", got)
	}
}

// tmux answers a pane target whose index does not exist with the active
// pane rather than an error, so a user config that numbers panes from 1
// would point every capture and keystroke at whatever pane has focus.
func TestEnsureBindingsPinsPaneNumbering(t *testing.T) {
	driver := requireTmux(t)
	if out, err := tmuxCmd("set-window-option", "-g", "pane-base-index", "1").CombinedOutput(); err != nil {
		t.Fatalf("set pane-base-index: %v: %s", err, out)
	}
	t.Cleanup(func() { tmuxCmd("set-window-option", "-g", "pane-base-index", "0").Run() })

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}

	out, err := tmuxCmd("show-window-options", "-gv", "pane-base-index").CombinedOutput()
	if err != nil {
		t.Fatalf("show-window-options: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "0" {
		t.Fatalf("pane-base-index = %q, want 0", got)
	}
}

func sessionOf(t *testing.T, detach, review, editor []string) keybind.Table {
	t.Helper()
	return keybind.DefaultSession().
		With(keybind.Detach, bindingOf(t, detach...)).
		With(keybind.Review, bindingOf(t, review...)).
		With(keybind.Editor, bindingOf(t, editor...))
}

func bindingOf(t *testing.T, specs ...string) keybind.Binding {
	t.Helper()
	keys := make([]keybind.Key, 0, len(specs))
	for _, spec := range specs {
		key, err := keybind.Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		keys = append(keys, key)
	}
	return keybind.Keys(keys...)
}

// ownedRootLines reads the root-table bindings the manager owns, keyed by
// the key name as unbind-key takes it.
func ownedRootLines(t *testing.T) map[string]string {
	t.Helper()
	bound, err := tmuxCmd("list-keys", "-T", "root").CombinedOutput()
	if err != nil {
		t.Fatalf("list root keys: %v: %s", err, bound)
	}
	owned := map[string]string{}
	for _, line := range strings.Split(string(bound), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.Contains(line, ownedBindingTest) {
			continue
		}
		owned[strings.ReplaceAll(fields[3], `\\`, `\`)] = line
	}
	return owned
}

func restoreDefaultKeys(t *testing.T, driver *Driver) {
	t.Helper()
	t.Cleanup(func() {
		driver.SetSessionKeys(keybind.DefaultSession())
		if err := driver.EnsureBindings(); err != nil {
			t.Errorf("restore default bindings: %v", err)
		}
	})
}

// The server outlives a config change, so the bindings the manager installs
// are the table's and nothing older: a key moved or turned off comes off
// the server, and moving back drops the keys it had moved to.
func TestEnsureBindingsFollowsTheKeyTable(t *testing.T) {
	driver := requireTmux(t)
	restoreDefaultKeys(t, driver)
	custom := sessionOf(t, []string{"f9", "alt+q"}, []string{"ctrl+g"}, nil)
	driver.SetSessionKeys(custom)
	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}
	owned := ownedRootLines(t)
	for _, key := range []string{"F9", "M-q"} {
		if line := owned[key]; !strings.Contains(line, "detach-client") || !strings.Contains(line, "send-keys "+key) {
			t.Errorf("%s should detach inside a session and pass through elsewhere, got %q", key, line)
		}
	}
	if line := owned["C-g"]; !strings.Contains(line, RequestReview) {
		t.Errorf("C-g should request the review, got %q", line)
	}
	for _, key := range []string{"C-q", `C-\`, "C-r", "F3"} {
		if line, bound := owned[key]; bound {
			t.Errorf("%s is off the table and should be unbound, got %q", key, line)
		}
	}

	driver.SetSessionKeys(keybind.DefaultSession())
	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings with defaults: %v", err)
	}
	owned = ownedRootLines(t)
	for _, key := range []string{"F9", "M-q", "C-g"} {
		if line, bound := owned[key]; bound {
			t.Errorf("%s should come off with the table that bound it, got %q", key, line)
		}
	}
	if line := owned[`C-\`]; !strings.Contains(line, "detach-client") || !strings.Contains(line, `send-keys C-\\\\`) {
		t.Errorf(`C-\ should detach and pass itself through, got %q`, line)
	}
	if line := owned["C-q"]; !strings.Contains(line, "detach-client") {
		t.Errorf("C-q should detach, got %q", line)
	}
	if line := owned["C-r"]; !strings.Contains(line, RequestReview) {
		t.Errorf("C-r should request the review, got %q", line)
	}
	if line := owned["F3"]; !strings.Contains(line, RequestEditor) {
		t.Errorf("F3 should request the editor, got %q", line)
	}
}

// Rebinding drops only what the manager put there: the user's own
// tmux.conf loads on this server too, and its bindings are not ours to
// remove.
func TestEnsureBindingsLeavesTheUsersOwnBindingsAlone(t *testing.T) {
	driver := requireTmux(t)
	if out, err := tmuxCmd("bind-key", "-n", "F9", "display-message", "mine").CombinedOutput(); err != nil {
		t.Fatalf("seed the user binding: %v: %s", err, out)
	}
	t.Cleanup(func() { tmuxCmd("unbind-key", "-n", "F9").Run() })

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}
	bound, err := tmuxCmd("list-keys", "-T", "root").CombinedOutput()
	if err != nil {
		t.Fatalf("list root keys: %v: %s", err, bound)
	}
	if !strings.Contains(string(bound), "display-message mine") {
		t.Fatalf("the user's F9 binding should survive, got:\n%s", bound)
	}
}

func TestAttachStatusRightNamesTheKeyTable(t *testing.T) {
	custom := sessionOf(t, []string{"f9", "alt+q"}, []string{"ctrl+g"}, nil)
	for _, tc := range []struct {
		name, primary, secondary string
		keys                     keybind.Table
		want                     string
	}{
		{"defaults", "C-b", "None", keybind.DefaultSession(), ` agent-manager · Ctrl+r = review · F3 = editor · Ctrl+q / Ctrl+\ / C-b d = back `},
		{"no prefix", "None", "None", keybind.DefaultSession(), ` agent-manager · Ctrl+r = review · F3 = editor · Ctrl+q / Ctrl+\ = back `},
		{"custom", "C-b", "None", custom, " agent-manager · Ctrl+g = review · F9 / Alt+q / C-b d = back "},
		{"prefix shadows a custom key", "M-q", "None", custom, " agent-manager · Ctrl+g = review · F9 / M-q d = back "},
	} {
		if got := attachStatusRight(tc.primary, tc.secondary, tc.keys); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// A session created under a custom table carries that table in its footer.
func TestSessionFooterNamesTheConfiguredKeys(t *testing.T) {
	driver := requireTmux(t)
	restoreDefaultKeys(t, driver)
	driver.SetSessionKeys(sessionOf(t, []string{"f9"}, []string{"ctrl+g"}, nil))
	id := "footerkeys"
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	right, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right: %v", err)
	}
	footer := string(right)
	if !strings.Contains(footer, "Ctrl+g = review") || !strings.Contains(footer, "F9") || strings.Contains(footer, "editor") || strings.Contains(footer, "Ctrl+q") {
		t.Fatalf("footer should name the configured keys only, got %q", footer)
	}
}
