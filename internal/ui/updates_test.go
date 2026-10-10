package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// handleMsg routes each message family to the file that owns it. A case
// body written here instead grows the router back into a monolith.
const maxHandleMsgLines = 60

func TestHandleMsgLineCap(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "updates.go", nil, 0)
	if err != nil {
		t.Fatalf("parse updates.go: %v", err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "handleMsg" {
			continue
		}
		lines := fset.Position(fn.Body.Rbrace).Line - fset.Position(fn.Body.Lbrace).Line - 1
		if lines > maxHandleMsgLines {
			t.Fatalf("handleMsg body has %d lines, cap is %d: route the message to its owner's file", lines, maxHandleMsgLines)
		}
		return
	}
	t.Fatal("handleMsg not found in updates.go")
}
