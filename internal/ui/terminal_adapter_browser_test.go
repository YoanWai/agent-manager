package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/termbrowser"
	"github.com/YoanWai/agent-manager/internal/testenv"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// fakeTerminalBrowser puts a terminal-browser on PATH that records its
// arguments and idles, so b opens a pane without starting a real engine.
// The model has to be built after it, so its tmux server inherits the PATH.
func fakeTerminalBrowser(t *testing.T) string {
	t.Helper()
	testenv.SkipPOSIXShell(t)
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s' \"$*\" > " + tmux.ShellQuote(argsFile) + "\nexec cat\n"
	if err := os.WriteFile(filepath.Join(dir, "terminal-browser"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake terminal-browser: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

func pressBrowserKey(t *testing.T, m *Model) {
	t.Helper()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
}

// openBrowserRow presses b and returns the browser row it made, which the
// cursor lands on.
func openBrowserRow(t *testing.T, m *Model) store.Session {
	t.Helper()
	pressBrowserKey(t, m)
	if m.errBar.text != "" {
		t.Fatalf("browser spawn reported %q", m.errBar.text)
	}
	sess, ok := m.selected()
	if !ok || !m.isBrowser(sess.Tool) {
		t.Fatalf("b should leave the cursor on the new browser, got %+v", sess)
	}
	return sess
}

func waitForFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		content, err := os.ReadFile(path)
		if err == nil && len(content) > 0 {
			return string(content)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never written: %v", path, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBKeyOpensABrowserUnderTheSelectedAgent(t *testing.T) {
	argsFile := fakeTerminalBrowser(t)
	m := buildModel(t)
	createSession(t, m, "coder", t.TempDir(), "")
	m.selectSessionRow(t, "coder")
	agent, _ := m.selected()

	browser := openBrowserRow(t, m)
	if browser.Tool != "browser" {
		t.Fatalf("tool = %q, want browser", browser.Tool)
	}
	if browser.ParentID != agent.ID || browser.Group != agent.Group {
		t.Fatalf("browser parent=%q group=%q, want %q / %q", browser.ParentID, browser.Group, agent.ID, agent.Group)
	}
	if want := "browser-coder"; browser.Name != want {
		t.Fatalf("browser name = %q, want %q", browser.Name, want)
	}
	if browser.BrowserURL != "" {
		t.Fatalf("b opens the start page, but the row stored %q", browser.BrowserURL)
	}
	if m.awaitingRename(browser) {
		t.Fatal("a browser gets no rename directive to wait for")
	}
	if got := waitForFile(t, argsFile); got != "open --no-merge" {
		t.Fatalf("terminal-browser ran with %q, want %q", got, "open --no-merge")
	}

	m.selectSessionRow(t, browser.Name)
	sibling := openBrowserRow(t, m)
	if sibling.ParentID != agent.ID {
		t.Fatalf("b on a browser row should open a sibling under %q, got parent %q", agent.ID, sibling.ParentID)
	}
}

// Without terminal-browser the key opens the install card instead of
// leaving a row whose pane exits at once, and the card holds the tab to
// open again once the installer puts the binary on PATH.
func TestBKeyOffersTheInstallerWithoutTerminalBrowser(t *testing.T) {
	testenv.SkipPOSIXShell(t)
	realTmux, err := exec.LookPath(tmux.Binary)
	if err != nil {
		t.Skip(tmux.Binary + " not installed")
	}
	dir := t.TempDir()
	if err := os.Symlink(realTmux, filepath.Join(dir, tmux.Binary)); err != nil {
		t.Fatalf("link tmux: %v", err)
	}
	t.Setenv("PATH", strings.Join([]string{dir, "/usr/bin", "/bin"}, string(os.PathListSeparator)))
	if _, err := exec.LookPath("terminal-browser"); err == nil {
		t.Skip("terminal-browser sits in a system directory")
	}
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())

	pressBrowserKey(t, m)
	if !hintOpen(m) {
		t.Fatalf("screen = %v, error bar = %q, want the install card", m.screenID(), m.errBar.text)
	}
	const installer = "curl -fsSL https://terminal-browser.sh/install | bash"
	fix := hintFix(m)
	if !strings.Contains(fix.Text, "terminal-browser is not installed") || !strings.Contains(fix.Text, installer) {
		t.Fatalf("card text = %q, want the missing terminal-browser and its installer", fix.Text)
	}
	if fix.Command != installer || fix.Script != installer {
		t.Fatalf("card command = %q, script = %q, want %q for both", fix.Command, fix.Script, installer)
	}
	if fix.Retry == nil {
		t.Fatal("the card holds no browser tab to open after the install")
	}
	for _, sess := range m.workspace.sessions {
		if m.isBrowser(sess.Tool) {
			t.Fatalf("a refused b still made %+v", sess)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "terminal-browser"), []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatalf("write terminal-browser: %v", err)
	}
	m.enqueueEffect(fix.Retry, 0, false)
	m.drainEffects(t)
	opened := false
	for _, sess := range m.workspace.sessions {
		if m.isBrowser(sess.Tool) {
			opened = true
			t.Cleanup(func() { m.services.tmux.Kill(sess.ID) })
		}
	}
	if !opened {
		t.Fatalf("the retry after the install opened no browser tab, error bar = %q", m.errBar.text)
	}
}

func TestBrowserRowsAreNotAgents(t *testing.T) {
	fakeTerminalBrowser(t)
	m := buildModel(t)
	m.prefs.comfortableRows = true
	createSession(t, m, "coder", t.TempDir(), "")
	m.selectSessionRow(t, "coder")
	browser := openBrowserRow(t, m)
	for index := range m.workspace.sessions {
		if m.workspace.sessions[index].ID == browser.ID {
			m.workspace.sessions[index].Status = status.Idle
		}
	}
	m.rebuildRows()

	if !m.isTool(browser.Tool) || m.isShell(browser.Tool) {
		t.Fatalf("browser tool: tool=%v shell=%v, want a tool that is not a shell", m.isTool(browser.Tool), m.isShell(browser.Tool))
	}
	lines := paintedRailLines(t, m, browser.Name)
	if len(lines) != 2 {
		t.Fatalf("comfortable browser row painted %d lines, want 2 like a shell", len(lines))
	}
	glyph := presentation.GlyphsFor(m.prefs.glyphMode).BrowserGlyph
	if row := ansi.Strip(m.layout.displayedRail.Lines[lines[0]].Text); glyph != "◎" || !strings.Contains(row, glyph) {
		t.Fatalf("browser row should wear ◎ (glyph set has %q):\n%s", glyph, row)
	}

	m.selectSessionRow(t, browser.Name)
	legend := m.rowLegend()
	if legend.Title != "Browser" {
		t.Fatalf("legend title = %q, want Browser", legend.Title)
	}
	if len(legend.Leads) != 0 || legend.Hint != "" {
		t.Fatalf("legend leads with %q on a browser row, which takes no prompt", legend.Leads)
	}
	for _, pair := range legend.Pairs {
		switch pair[0] {
		case "space", "y", "ctrl+r", "f":
			t.Fatalf("legend offers %q on a browser row, which has no conversation", pair[0])
		}
	}

	m.copyReplySelected()
	if want := browser.Name + " is a browser, not an agent"; !strings.Contains(m.errBar.text, want) {
		t.Fatalf("copying a browser's reply reported %q, want %q", m.errBar.text, want)
	}

	m = railMouseAtLine(t, m, lines[0], tea.MouseActionPress, tea.MouseButtonRight)
	preparedView(m)
	menu := ansi.Strip(strings.Join(m.layout.displayedRail.Menu, "\n"))
	if !strings.Contains(menu, "Attach") {
		t.Fatalf("browser menu should attach:\n%s", menu)
	}
	for _, refused := range []string{"Quick prompt mode", "Fork", "Copy last reply", "Review changes"} {
		if strings.Contains(menu, refused) {
			t.Fatalf("browser menu offers %q:\n%s", refused, menu)
		}
	}
}

func TestAgentMenuOffersANewBrowser(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "coder", t.TempDir(), "")
	m = railMouseAtLine(t, m, paintedRailLines(t, m, "coder")[0], tea.MouseActionPress, tea.MouseButtonRight)
	preparedView(m)
	menu := ansi.Strip(strings.Join(m.layout.displayedRail.Menu, "\n"))
	if !strings.Contains(menu, "New terminal") || !strings.Contains(menu, "New browser") {
		t.Fatalf("agent menu should offer a terminal and a browser:\n%s", menu)
	}
	if line := strings.Fields(ansi.Strip(m.layout.displayedRail.Menu[menuEntry(t, m, "New browser")+1])); len(line) < 3 || line[len(line)-2] != "b" {
		t.Fatalf("New browser should name its key b, got %q", line)
	}
}

// A browser the manager starts has no terminal attached to answer
// terminal-browser's frame probe, so the manager hands the answer to every
// pane on its server, and takes it back where it would be wrong.
func TestSyncBrowserFramesFollowsTheManagersTerminal(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "coder", t.TempDir(), "")
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv(termbrowser.FramesEnv, "")

	t.Setenv("TERM_PROGRAM", "ghostty")
	m.syncBrowserFrames()
	out, err := tmuxCmd(t, "show-environment", "-g", termbrowser.FramesEnv).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != termbrowser.FramesEnv+"=shm" {
		t.Fatalf("in Ghostty: %q %v", out, err)
	}
	if got := os.Getenv(termbrowser.FramesEnv); got != "shm" {
		t.Fatalf("a server this manager starts would inherit %q, want shm", got)
	}

	t.Setenv("TERM_PROGRAM", "iTerm.app")
	m.syncBrowserFrames()
	out, _ = tmuxCmd(t, "show-environment", "-g", termbrowser.FramesEnv).CombinedOutput()
	if strings.Contains(string(out), termbrowser.FramesEnv+"=") {
		t.Fatalf("in iTerm2 the server still carries %q", out)
	}
	if _, set := os.LookupEnv(termbrowser.FramesEnv); set {
		t.Fatal("in iTerm2 the manager's own environment still sets the frames")
	}
}
