package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

type effectDeclarations struct {
	files []*ast.File
}

func parseEffectDeclarations(t *testing.T) effectDeclarations {
	t.Helper()
	fset := token.NewFileSet()
	packages, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, file := range packages["ui"].Files {
		files = append(files, file)
	}
	return effectDeclarations{files: files}
}

// A request a feature package declares runs only once bindEffect binds its
// services, and its result reaches the model only through applyEffect.
func TestEveryPackageEffectIsBoundAndApplied(t *testing.T) {
	executor := parseRootFile(t, "effects_adapter_executor.go")
	bound := map[string]bool{}
	applied := map[string]bool{}
	ast.Inspect(executor, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.IndexExpr:
			if selector, ok := node.X.(*ast.SelectorExpr); ok && selector.Sel.Name == "Bind" {
				bound[types(node.Index)] = true
			}
		case *ast.FuncLit:
			if node.Type.Results != nil && len(node.Type.Results.List) == 1 {
				bound[types(node.Type.Results.List[0].Type)] = true
			}
		case *ast.CaseClause:
			for _, expr := range node.List {
				applied[types(expr)] = true
			}
		}
		return true
	})

	for _, effect := range packageEffects(t) {
		if effect.result && !applied[effect.name] {
			t.Errorf("%s is an effect result with no case in applyEffect", effect.name)
		}
		if effect.services != "" && !bound[effect.services] {
			t.Errorf("%s runs against %s, which bindEffect never binds", effect.name, effect.services)
		}
	}
}

type packageEffect struct {
	name     string
	result   bool
	services string
}

// packageEffects lists the effect results and requests the child packages of
// internal/ui declare, spelled with the alias the root imports them under.
func packageEffects(t *testing.T) []packageEffect {
	t.Helper()
	aliases := rootImportAliases(t)
	var effects []packageEffect
	named := map[string]bool{}
	runs := map[string]string{}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Dir(path) == "." || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		alias, ok := aliases[filepath.ToSlash(filepath.Dir(path))]
		if !ok {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil {
				continue
			}
			receiver := alias + "." + strings.TrimPrefix(types(function.Recv.List[0].Type), "*")
			switch function.Name.Name {
			case "EffectResult":
				effects = append(effects, packageEffect{name: receiver, result: true})
			case "EffectName":
				named[receiver] = true
			case "Run":
				if params := function.Type.Params.List; len(params) == 1 {
					runs[receiver] = alias + "." + types(params[0].Type)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for receiver, services := range runs {
		if named[receiver] {
			effects = append(effects, packageEffect{name: receiver, services: services})
		}
	}
	return effects
}

// rootImportAliases maps each internal/ui child package directory, relative
// to this one, to the name the root imports it under.
func rootImportAliases(t *testing.T) map[string]string {
	t.Helper()
	const prefix = "github.com/YoanWai/agent-manager/internal/ui/"
	aliases := map[string]string{}
	for _, file := range parseEffectDeclarations(t).files {
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if !strings.HasPrefix(path, prefix) {
				continue
			}
			dir := strings.TrimPrefix(path, prefix)
			name := filepath.Base(dir)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			aliases[dir] = name
		}
	}
	return aliases
}

func parseRootFile(t *testing.T, name string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	return file
}
