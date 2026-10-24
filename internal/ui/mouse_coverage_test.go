package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// mouseOpeners mounts every surface the way the user does, on a list with
// a selected session. A surface type missing here fails, so a new surface
// is driven by the mouse on purpose.
var mouseOpeners = map[string]func(t *testing.T, m *Model){
	"dialogs.ConfirmSurface": func(t *testing.T, m *Model) { m.openConfirm(confirmTarget{action: actionKill, label: "kill alpha?"}) },
	"dialogs.MoveSurface":    func(t *testing.T, m *Model) { m.openMove() },
	"dialogs.RenameSurface":  func(t *testing.T, m *Model) { m.openRename() },
	"dialogs.ForkSurface":    func(t *testing.T, m *Model) { m.push(newForkSurface(m, railSelectedSession(m), 1)) },
	"dialogs.LaunchHintSurface": func(t *testing.T, m *Model) {
		m.openLaunchHint(uiinstall.Fix{Text: "tool is missing", Command: "true", Binary: "tool"})
	},
	"help.Surface":             func(t *testing.T, m *Model) { m.openHelp() },
	"notices.Surface":          func(t *testing.T, m *Model) { m.Update(key("M")) },
	"settings.Surface":         func(t *testing.T, m *Model) { m.applyCmd(t, m.openSettings()) },
	"dialogs.GroupFormSurface": func(t *testing.T, m *Model) { m.openGroupForm() },
	"connections.Surface":      func(t *testing.T, m *Model) { m.openConnectionDialog(remote.Connection{}) },
	"quick.Surface":            func(t *testing.T, m *Model) { m.applyCmd(t, m.openQuickMode()) },
	"spawn.Surface":            func(t *testing.T, m *Model) { m.applyCmd(t, m.openForm()) },
	"geometry.Surface":         func(t *testing.T, m *Model) { m.enterResizeMode() },
	"focus.Surface":            func(t *testing.T, m *Model) { m.push(newFocusSurface(m)) },
	"focus.Saved": func(t *testing.T, m *Model) {
		m.push(newFocusSurface(m))
		m.push(uifocus.OpenSaved(focusSavedRoot{m}, 0, m.spawnStyles()))
	},
	"review.Surface": func(t *testing.T, m *Model) {
		m.base = baseReview
		m.push(uireview.NewSurface(reviewRoot{m}, []uireview.PickerRow{{Label: "main"}}, "branch", uireview.PickRepo, "", uireview.PickerSource{}, "", uireview.RenderContext{}, ""))
	},
}

// Every surface answers the mouse: a wheel and a click over it neither
// panic nor touch the stack under it.
func TestEverySurfaceAnswersTheMouse(t *testing.T) {
	for _, name := range declaredSurfaces(t) {
		open, ok := mouseOpeners[name]
		if !ok {
			t.Errorf("%s has no opener in mouseOpeners: drive it by the mouse here", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			m := buildModel(t)
			m.layout.width, m.layout.height = 120, 40
			createSession(t, m, "alpha", t.TempDir(), "")
			m.selectSessionRow(t, "alpha")
			open(t, m)
			top, ok := m.top()
			if !ok || surfaceName(top) != name {
				t.Fatalf("the opener did not put %s on top, stack %d, err %q", name, len(m.surfaces), m.errBar.text)
			}
			below := len(m.surfaces) - 1
			preparedView(m)
			x, y := m.layout.width/2, m.layout.height/2
			for _, msg := range []tea.MouseMsg{
				{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress},
				{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
				{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease},
			} {
				m.Update(msg)
				if len(m.surfaces) < below {
					t.Fatalf("a mouse event over %s popped the stack under it", name)
				}
			}
		})
	}
	for name := range mouseOpeners {
		if !contains(declaredSurfaces(t), name) {
			t.Errorf("mouseOpeners names %s, which no longer implements surface", name)
		}
	}
}

// surfaceName names a surface the way declaredSurfaces does: bare in the
// root, qualified by its package in a feature's.
func surfaceName(s surface) string {
	typ := reflect.TypeOf(s).Elem()
	if pkg := path.Base(typ.PkgPath()); pkg != "ui" {
		return pkg + "." + typ.Name()
	}
	return typ.Name()
}

// declaredSurfaces lists the types that declare a Leave(surfaceHost) method on a
// pointer receiver, in the root and in the feature packages under it: every
// surface does, and nothing else.
func declaredSurfaces(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	dirs := []string{"."}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "testdata" {
			dirs = append(dirs, entry.Name())
		}
	}
	var names []string
	for _, dir := range dirs {
		fset := token.NewFileSet()
		packages, err := parser.ParseDir(fset, dir, func(info fs.FileInfo) bool {
			return !strings.HasSuffix(info.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		for pkg, parsed := range packages {
			for _, file := range parsed.Files {
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Recv == nil || fn.Name.Name != "Leave" || !takesHost(fn) {
						continue
					}
					if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
						if ident, ok := star.X.(*ast.Ident); ok {
							name := ident.Name
							if pkg != "ui" {
								name = pkg + "." + name
							}
							names = append(names, name)
						}
					}
				}
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("found no surfaces")
	}
	return names
}

// takesHost is whether fn's one parameter is the stack's host, which tells a
// surface's Leave from another type's.
func takesHost(fn *ast.FuncDecl) bool {
	params := fn.Type.Params.List
	if len(params) != 1 {
		return false
	}
	switch typ := params[0].Type.(type) {
	case *ast.Ident:
		return typ.Name == "surfaceHost"
	case *ast.SelectorExpr:
		return typ.Sel.Name == "Host"
	}
	return false
}
