package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// On each base screen, and on a focused session, ctrl+c pressed the way a
// terminal delivers it, through Update, quits where the manager owns the
// keyboard and passes to the agent where it does not.
func TestCtrlCOnEveryBase(t *testing.T) {
	for name, tc := range map[string]struct {
		open func(m *Model)
		quit bool
	}{
		"list":   {func(*Model) {}, true},
		"review": {func(m *Model) { m.base = baseReview }, true},
		"focus":  {func(m *Model) { m.push(newFocusSurface(m)) }, false},
	} {
		t.Run(name, func(t *testing.T) {
			m := buildModel(t)
			tc.open(m)
			m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			if m.effects.quitting != tc.quit {
				t.Fatalf("ctrl+c quitting = %t, want %t", m.effects.quitting, tc.quit)
			}
		})
	}
}

// Every card the manager puts over the list leaves ctrl+c alone: the
// manager owns the keyboard under it, so the key quits as it does with no
// card up, whether or not the card holds a draft. #855 was a card that
// decided this for itself; the router decides it once here.
func TestCtrlCQuitsThroughEveryCardOverTheList(t *testing.T) {
	for name, open := range map[string]func(t *testing.T, m *Model){
		"confirm": func(t *testing.T, m *Model) { m.openConfirm(confirmTarget{action: actionKill, label: "kill alpha?"}) },
		"move":    func(t *testing.T, m *Model) { m.openMove() },
		"rename":  func(t *testing.T, m *Model) { m.openRename() },
		"fork":    func(t *testing.T, m *Model) { m.push(newForkSurface(m, railSelectedSession(m), 1)) },
		"launchhint": func(t *testing.T, m *Model) {
			m.openLaunchHint(launchFix{text: "tool is missing", command: "true", script: "true", binary: "tool"})
		},
		"help":       func(t *testing.T, m *Model) { m.openHelp() },
		"notices":    func(t *testing.T, m *Model) { m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")}) },
		"settings":   func(t *testing.T, m *Model) { m.applyCmd(t, m.openSettings()) },
		"group form": func(t *testing.T, m *Model) { m.openGroupForm() },
		"connection": func(t *testing.T, m *Model) { m.openConnectionDialog(remote.Connection{}) },
	} {
		t.Run(name, func(t *testing.T) {
			m := buildModel(t)
			m.layout.width, m.layout.height = 120, 40
			createSession(t, m, "alpha", t.TempDir(), "")
			m.selectSessionRow(t, "alpha")
			open(t, m)
			if len(m.surfaces) == 0 {
				t.Fatalf("%s: setup put nothing on the stack", name)
			}
			m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			if !m.effects.quitting {
				t.Fatalf("%s: ctrl+c over the card did not quit", name)
			}
		})
	}
}

// The saved prompts list over a focused session is the manager's card, not
// the agent's pane, so ctrl+c closes it the way esc does. It neither quits
// the manager nor reaches the agent from behind the list.
func TestCtrlCClosesTheSavedListOverFocus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "saved-ctrlc", t.TempDir(), "")
	m.selectSessionRow(t, "saved-ctrlc")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("enter did not focus, screen = %v", m.screenID())
	}
	m.push(uifocus.OpenSaved(focusSavedRoot{m}, 0, m.spawnStyles()))
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.effects.quitting || !m.focused() || m.savedListOpen() {
		t.Fatalf("quitting=%t screen = %v list open=%t", m.effects.quitting, m.screenID(), m.savedListOpen())
	}
	if m.effects.input.active != nil || len(m.effects.input.pending) > 0 {
		t.Fatal("ctrl+c reached the agent from behind the list")
	}
}

// A drain already under way leaves on a second ctrl+c from any surface,
// a focused session included: by then the agent has nothing to cancel.
func TestCtrlCAbandonsADrainFromFocus(t *testing.T) {
	m := buildModel(t)
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"a"}}}, 0, false)
	m.requestQuit()
	if !m.effects.quitting || m.effects.main.active == nil {
		t.Fatal("quit did not begin draining")
	}
	m.push(newFocusSurface(m))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c during a drain was discarded")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c during a drain did not exit")
	}
}

// A click on a painted ctrl+c presses the key the way typing it does, so it
// reaches the same router and not a surface's own idea of quit.
func TestClickedCtrlCTakesTheRouter(t *testing.T) {
	m := buildModel(t)
	// A frame repaints the hits on every Update, so the click is run on a
	// hit placed by hand, the way routeMouse runs it on a release.
	m.layout.keyHits = keyHits{}.add(0, 6, 0, "ctrl+c")
	release := tea.MouseMsg{X: 1, Y: 0, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
	target, ok := m.clickTargetAt(release)
	if !ok || target.kind != targetKey {
		t.Fatalf("painted ctrl+c is not a click target: %+v", target)
	}
	m.click(release, target)
	if !m.effects.quitting {
		t.Fatal("a click on ctrl+c did not quit")
	}
}

// keys.go is the one place that decides what ctrl+c does. A surface that
// compares the key itself is a second opinion, and two opinions are how a
// focused session lost the key to the manager's quit. Display strings that
// name the key in a legend are not comparisons and stay free.
func TestCtrlCIsDecidedInKeysGo(t *testing.T) {
	var router int
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, at := range ctrlCComparisons(file) {
			if path == "keys.go" {
				router++
				continue
			}
			t.Errorf("%s:%d compares ctrl+c; route it through routeQuit in keys.go", path, fset.Position(at).Line)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/ui: %v", err)
	}
	if router == 0 {
		t.Fatal("keys.go compares ctrl+c nowhere: the walk or the router moved")
	}
}

// ctrlCComparisons are the places a file tests a key against ctrl+c: an
// == or != against the name or tea.KeyCtrlC, or a case that lists either.
func ctrlCComparisons(file *ast.File) []token.Pos {
	var found []token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.BinaryExpr:
			if (n.Op == token.EQL || n.Op == token.NEQ) && (isCtrlC(n.X) || isCtrlC(n.Y)) {
				found = append(found, n.Pos())
			}
		case *ast.CaseClause:
			for _, expr := range n.List {
				if isCtrlC(expr) {
					found = append(found, expr.Pos())
				}
			}
		}
		return true
	})
	return found
}

func isCtrlC(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		value, _ := strconv.Unquote(e.Value)
		return e.Kind == token.STRING && strings.EqualFold(value, "ctrl+c")
	case *ast.SelectorExpr:
		return e.Sel.Name == "KeyCtrlC"
	}
	return false
}
