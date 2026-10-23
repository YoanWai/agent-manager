package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"testing"
)

// maxHostMethods keeps a feature's host to the facts it uses; a feature that
// needs more returns a typed request for a root adapter instead.
const maxHostMethods = 8

// forbiddenHostTypes would let a feature reach the whole root or a runtime
// driver through its host.
var forbiddenHostTypes = []string{"Model", "services", "effectServices", "effectRequest", "store.Store", "tmux.Driver", "git.Driver"}

func TestFeatureHostsStayNarrow(t *testing.T) {
	fset := token.NewFileSet()
	packages, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, file := range packages["ui"].Files {
		files = append(files, file)
	}
	hosts, violations := hostViolations(files)
	if hosts == 0 {
		t.Fatal("found no feature hosts")
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

func TestHostViolationsCatchBroadHosts(t *testing.T) {
	source := `package ui
type wideHost interface { a(); b(); c(); d(); e(); f(); g(); h(); i() }
type rootHost interface { root() *Model }
type sneakyHost interface { rootHost }
type driverHost interface { run(d *tmux.Driver) error }
type fineHost interface { size() (int, int) }`
	file, err := parser.ParseFile(token.NewFileSet(), "hosts.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, violations := hostViolations([]*ast.File{file})
	joined := strings.Join(violations, "\n")
	for _, want := range []string{"wideHost has 9 methods", "rootHost.root passes Model", "sneakyHost.root passes Model", "driverHost.run passes tmux.Driver"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "fineHost") {
		t.Errorf("a narrow host was flagged:\n%s", joined)
	}
}

// hostViolations reports each *Host interface in files whose method set,
// embedded hosts included, is over the cap or passes a forbidden type.
func hostViolations(files []*ast.File) (hosts int, violations []string) {
	declared := map[string]*ast.InterfaceType{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			if spec, ok := node.(*ast.TypeSpec); ok {
				if iface, ok := spec.Type.(*ast.InterfaceType); ok && strings.HasSuffix(spec.Name.Name, "Host") {
					declared[spec.Name.Name] = iface
				}
			}
			return true
		})
	}
	var methods func(iface *ast.InterfaceType) []*ast.Field
	methods = func(iface *ast.InterfaceType) []*ast.Field {
		var all []*ast.Field
		for _, field := range iface.Methods.List {
			if len(field.Names) == 0 {
				if embedded, ok := field.Type.(*ast.Ident); ok && declared[embedded.Name] != nil {
					all = append(all, methods(declared[embedded.Name])...)
					continue
				}
				violations = append(violations, "a host embeds "+types(field.Type)+", which this check cannot see into")
				continue
			}
			all = append(all, field)
		}
		return all
	}
	isWord := func(r rune) bool {
		return r == '.' || r == '_' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
	}
	for name, iface := range declared {
		set := methods(iface)
		if len(set) > maxHostMethods {
			violations = append(violations, fmt.Sprintf("%s has %d methods, cap is %d: return a typed request for a root adapter instead", name, len(set), maxHostMethods))
		}
		for _, method := range set {
			signature := types(method.Type)
			for _, word := range strings.FieldsFunc(signature, func(r rune) bool { return !isWord(r) }) {
				if slices.Contains(forbiddenHostTypes, word) {
					violations = append(violations, fmt.Sprintf("%s.%s passes %s: %s", name, method.Names[0].Name, word, signature))
				}
			}
		}
	}
	sort.Strings(violations)
	return len(declared), violations
}

// types renders a type expression as source text.
func types(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return "*" + types(expr.X)
	case *ast.SelectorExpr:
		return types(expr.X) + "." + expr.Sel.Name
	case *ast.ArrayType:
		return "[]" + types(expr.Elt)
	case *ast.MapType:
		return "map[" + types(expr.Key) + "]" + types(expr.Value)
	case *ast.FuncType:
		var parts []string
		for _, list := range []*ast.FieldList{expr.Params, expr.Results} {
			if list == nil {
				continue
			}
			for _, field := range list.List {
				parts = append(parts, types(field.Type))
			}
		}
		return "func(" + strings.Join(parts, ", ") + ")"
	case *ast.Ellipsis:
		return "..." + types(expr.Elt)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.ChanType:
		return "chan " + types(expr.Value)
	}
	return "?"
}
