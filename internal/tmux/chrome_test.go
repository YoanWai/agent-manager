package tmux

import (
	"os"
	"strings"
	"sync"
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

// A session created after a theme is published, but before any push has run,
// still opens on that theme: Create applies the recorded value in its own
// command list rather than relying on a separate push having landed.
func TestCreateAppliesPublishedThemeWithoutAPush(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })
	driver.PublishPaneTheme(PaneTheme{Background: "#1e1e2e", ColorFgBg: "15;0"})

	id := "pub" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if got, want := globalWindowStyle(t), "bg=#1e1e2e"; got != want {
		t.Fatalf("window-style = %q, want %q", got, want)
	}
}

// Concurrent pushes are latest-wins: whichever runs last writes the theme
// published last, not an older one it was spawned for. The lock serializes
// the writes and each push sends the current published value, so the server
// settles on the final publish however the goroutines interleave.
func TestPushPaneThemeIsLatestWins(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })

	// A session with no windows exits at once, so one holds the server up
	// for the global option to stick to.
	id := "race" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	driver.PublishPaneTheme(PaneTheme{Background: "#101010", ColorFgBg: "15;0"})
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	backgrounds := []string{"#111111", "#222222", "#333333", "#444444", "#eff1f5"}
	var wg sync.WaitGroup
	for _, bg := range backgrounds {
		driver.PublishPaneTheme(PaneTheme{Background: bg, ColorFgBg: "15;0"})
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := driver.PushPaneTheme(); err != nil {
				t.Errorf("PushPaneTheme: %v", err)
			}
		}()
	}
	wg.Wait()

	last := backgrounds[len(backgrounds)-1]
	if got, want := globalWindowStyle(t), "bg="+last; got != want {
		t.Fatalf("window-style = %q, want the last published theme %q", got, want)
	}
}

// Create shares the push lock with PushPaneTheme, so a session opened while a
// newer theme is being pushed cannot reset the server to the theme Create
// loaded. The seam runs while Create holds the lock: it publishes a newer
// theme and starts a push, which blocks on the lock until Create's write
// lands, then writes the newer theme last. Without the shared lock the push
// runs during the seam and Create's stale write clobbers it.
func TestCreateSerializesWithPushPaneTheme(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })

	stamp := strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	hold := "hold" + stamp
	driver.PublishPaneTheme(PaneTheme{Background: "#101010", ColorFgBg: "15;0"})
	if err := driver.Create(hold, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create hold session: %v", err)
	}
	t.Cleanup(func() { driver.Kill(hold) })

	const newer = "#eff1f5"
	var pushed sync.WaitGroup
	afterCreateThemeLoad = func() {
		driver.PublishPaneTheme(PaneTheme{Background: newer, ColorFgBg: "0;15"})
		pushed.Add(1)
		go func() {
			defer pushed.Done()
			if err := driver.PushPaneTheme(); err != nil {
				t.Errorf("PushPaneTheme: %v", err)
			}
		}()
		// Give the push goroutine time to reach the lock, so the serialization
		// under test is what orders the two writes rather than this timing.
		time.Sleep(100 * time.Millisecond)
	}
	t.Cleanup(func() { afterCreateThemeLoad = nil })

	raced := "raced" + stamp
	if err := driver.Create(raced, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create raced session: %v", err)
	}
	t.Cleanup(func() { driver.Kill(raced) })
	pushed.Wait()

	if got, want := globalWindowStyle(t), "bg="+newer; got != want {
		t.Fatalf("window-style = %q, want the newer pushed theme %q", got, want)
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
