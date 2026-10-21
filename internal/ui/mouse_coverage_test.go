package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"
)

// Every screen works by mouse as well as by keyboard. A card's body takes
// clicks in clickCard and its list takes the wheel in wheelCard, or the
// mode is listed below with the reason it does without. A new mode fails
// here until it does one or the other.
var (
	noCardClick = map[string]string{
		"modeList":          "the list takes the mouse in listMouse",
		"modeDiff":          "Review takes the mouse in reviewMouse",
		"modeRename":        "the name is typed into its row, and the list holds still",
		"modeConfirmDelete": "a yes or no question: its painted keys take the clicks",
		"modeFork":          "one field, always focused: its painted keys take the clicks",
	}
	noCardWheel = map[string]string{
		"modeList":          "the list takes the wheel in listMouse",
		"modeDiff":          "Review takes the wheel in reviewMouse",
		"modeRename":        "the name is typed into its row, and the list holds still",
		"modeConfirmDelete": "nothing to scroll",
		"modeFork":          "nothing to scroll",
		"modeLaunchHint":    "nothing to scroll",
		"modeConnection":    "three fields and nothing to scroll",
	}
)

func TestEveryModeAnswersTheMouse(t *testing.T) {
	modes := declaredModes(t)
	for _, check := range []struct {
		handler string
		exempt  map[string]string
	}{{"clickCard", noCardClick}, {"wheelCard", noCardWheel}} {
		handled := modeCases(t, "mouse.go", check.handler)
		for _, name := range modes {
			_, exempt := check.exempt[name]
			switch covered := slices.Contains(handled, name); {
			case !covered && !exempt:
				t.Errorf("%s has no case in %s: handle the mouse there, or add it to the exemptions in this file with why it does without", name, check.handler)
			case covered && exempt:
				t.Errorf("%s is handled in %s and also exempted from it: drop the exemption", name, check.handler)
			}
		}
		for name := range check.exempt {
			if !slices.Contains(modes, name) {
				t.Errorf("%s exempts %s, which is no longer a mode", check.handler, name)
			}
		}
	}
}

// declaredModes lists the constants of type mode in model.go.
func declaredModes(t *testing.T) []string {
	t.Helper()
	file := parseUIFile(t, "model.go")
	var names []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST || len(gen.Specs) == 0 {
			continue
		}
		first := gen.Specs[0].(*ast.ValueSpec)
		if ident, ok := first.Type.(*ast.Ident); !ok || ident.Name != "mode" {
			continue
		}
		for _, spec := range gen.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				names = append(names, name.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("model.go declares no mode constants")
	}
	return names
}

// modeCases lists the modes named in the case clauses of fn's switch on
// m.mode.
func modeCases(t *testing.T, path, fn string) []string {
	t.Helper()
	var names []string
	found := false
	for _, decl := range parseUIFile(t, path).Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name.Name != fn {
			continue
		}
		found = true
		ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
			swtch, ok := node.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			if tag, ok := swtch.Tag.(*ast.SelectorExpr); !ok || tag.Sel.Name != "mode" {
				return true
			}
			for _, stmt := range swtch.Body.List {
				for _, expr := range stmt.(*ast.CaseClause).List {
					if ident, ok := expr.(*ast.Ident); ok {
						names = append(names, ident.Name)
					}
				}
			}
			return true
		})
	}
	if !found {
		t.Fatalf("%s declares no %s", path, fn)
	}
	return names
}

func parseUIFile(t *testing.T, path string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return file
}
