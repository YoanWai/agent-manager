package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Production code reaches the status bar through reportErr, clearErr,
// reportDone and reportWarn, so a feature type can take them from its host
// instead of the whole bar.
func TestStatusBarWritesGoThroughItsMethods(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "status_bar.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range assign.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "text" {
					if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "errBar" {
						t.Errorf("%s writes the status bar directly; use reportErr or clearErr", fset.Position(assign.Pos()))
					}
				}
			}
			return true
		})
	}
}
