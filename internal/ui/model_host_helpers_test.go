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

// parseGoPackage parses the non-test Go sources in dir, which the
// architecture tests read declaration by declaration.
func parseGoPackage(t *testing.T, dir string, mode parser.Mode) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, mode)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	return files
}
