package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// mouseOpeners mounts every surface the way the user does, on a list with
// a selected session. A surface type missing here fails, so a new surface
// is driven by the mouse on purpose.
var mouseOpeners = map[string]func(t *testing.T, m *Model){
	"confirmSurface": func(t *testing.T, m *Model) { m.openConfirm(confirmTarget{action: actionKill, label: "kill alpha?"}) },
	"moveSurface":    func(t *testing.T, m *Model) { m.openMove() },
	"renameSurface":  func(t *testing.T, m *Model) { m.openRename() },
	"forkSurface":    func(t *testing.T, m *Model) { m.push(newForkSurface(railSelectedSession(m), 1)) },
	"launchHintSurface": func(t *testing.T, m *Model) {
		m.openLaunchHint(launchFix{text: "tool is missing", command: "true", binary: "tool"})
	},
	"helpSurface":        func(t *testing.T, m *Model) { m.openHelp() },
	"noticesSurface":     func(t *testing.T, m *Model) { m.Update(key("M")) },
	"settingsSurface":    func(t *testing.T, m *Model) { m.applyCmd(t, m.openSettings()) },
	"groupFormSurface":   func(t *testing.T, m *Model) { m.openGroupForm() },
	"connectionsSurface": func(t *testing.T, m *Model) { m.openConnectionDialog(remote.Connection{}) },
	"quickSurface":       func(t *testing.T, m *Model) { m.applyCmd(t, m.openQuickMode()) },
	"spawnSurface":       func(t *testing.T, m *Model) { m.applyCmd(t, m.openForm()) },
	"resizeSurface":      func(t *testing.T, m *Model) { m.enterResizeMode() },
	"focusSurface":       func(t *testing.T, m *Model) { m.push(&focusSurface{}) },
	"focusSaved": func(t *testing.T, m *Model) {
		m.push(&focusSurface{})
		m.push(newFocusSaved(0, m.spawnStyles()))
	},
	"reviewPicker": func(t *testing.T, m *Model) {
		m.base = baseReview
		m.push(newReviewPicker([]uireview.PickerRow{{Label: "main"}}, "branch", uireview.PickRepo, "", uireview.PickerSource{}, "", uireview.RenderContext{}, ""))
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
			if !ok || reflect.TypeOf(top).Elem().Name() != name {
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

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// declaredSurfaces lists the types in the package that declare a Leave
// method on a pointer receiver: every surface does, and nothing else.
func declaredSurfaces(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	packages, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, file := range packages["ui"].Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Leave" {
				continue
			}
			if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
				if ident, ok := star.X.(*ast.Ident); ok {
					names = append(names, ident.Name)
				}
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("found no surfaces")
	}
	return names
}
