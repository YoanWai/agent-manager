// Command check-ui-moves verifies that a UI-only file move preserves Go
// declarations and comments. Imports and file locations may change.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const uiRoot = "internal/ui"

type declaration struct {
	Key        string `json:"key"`
	Hash       string `json:"hash"`
	Constraint string `json:"constraint,omitempty"`
}

type count struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

type snapshot struct {
	Declarations         []declaration       `json:"declarations"`
	CommentTokens        []count             `json:"comment_tokens"`
	ExportedNames        []string            `json:"exported_names"`
	InitFunctions        []string            `json:"init_functions"`
	VariableInitializers []string            `json:"variable_initializers"`
	CallInitializers     []string            `json:"call_initializers"`
	CompilerDirectives   []count             `json:"compiler_directives"`
	Locations            map[string][]string `json:"-"`
}

type move struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
	From string `json:"from"`
	To   string `json:"to"`
}

type moveReport struct {
	Schema int    `json:"schema"`
	Base   string `json:"base"`
	Target string `json:"target"`
	Moves  []move `json:"moves"`
}

type fileContent struct {
	Path string
	Data []byte
}

func main() {
	excludes := flag.String("exclude", "", "comma-separated UI basenames or paths to omit")
	mapOut := flag.String("map-out", "", "write the declaration location map as JSON")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: go run ./tools/architecture/check-ui-moves [flags] BASE TARGET\n")
		fmt.Fprintf(flag.CommandLine.Output(), "TARGET may be WORKTREE; git refs are otherwise read with git show.\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	baseRef, targetRef := flag.Arg(0), flag.Arg(1)
	excluded := parseExcludes(*excludes)
	base := loadSnapshot(baseRef, excluded)
	target := loadSnapshot(targetRef, excluded)
	if err := compareSnapshots(base, target); err != nil {
		fatalf("UI move check failed: %v", err)
	}
	if !equal(base.VariableInitializers, target.VariableInitializers) {
		fmt.Printf("NOTICE: package variable source order changed (%d initialized specs); review dependency and call-initializer evidence\n", len(target.VariableInitializers))
	}
	if !equal(base.CallInitializers, target.CallInitializers) {
		fmt.Printf("NOTICE: call-bearing initializer order changed\n  base:   %s\n  target: %s\n", strings.Join(base.CallInitializers, ", "), strings.Join(target.CallInitializers, ", "))
	}
	if *mapOut != "" {
		writeMoveReport(*mapOut, baseRef, targetRef, base, target)
	}
	fmt.Printf("PASS: %d declarations, %d comment tokens, %d exported names, and %d init functions preserved\n", len(target.Declarations), total(target.CommentTokens), len(target.ExportedNames), len(target.InitFunctions))
}

func loadSnapshot(ref string, excluded map[string]bool) snapshot {
	files := loadFiles(ref, excluded)
	result := snapshot{Locations: make(map[string][]string)}
	commentCounts := make(map[string]int)
	directiveCounts := make(map[string]int)
	for _, source := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, source.Path, source.Data, parser.ParseComments)
		must(err)
		constraint := buildConstraint(file)
		for _, group := range file.Comments {
			for _, comment := range group.List {
				commentCounts[comment.Text]++
				if strings.HasPrefix(comment.Text, "//go:") {
					directiveCounts[comment.Text]++
				}
			}
		}
		for _, node := range file.Decls {
			if gen, ok := node.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
				continue
			}
			start := node.Pos()
			switch item := node.(type) {
			case *ast.FuncDecl:
				if item.Doc != nil {
					start = item.Doc.Pos()
				}
			case *ast.GenDecl:
				if item.Doc != nil {
					start = item.Doc.Pos()
				}
			}
			text := string(source.Data[fset.Position(start).Offset:fset.Position(node.End()).Offset])
			key, exported, variables, calls, initFunction := inspectDeclaration(node, text)
			hash := digest(text)
			result.Declarations = append(result.Declarations, declaration{Key: key, Hash: hash, Constraint: constraint})
			result.ExportedNames = append(result.ExportedNames, exported...)
			result.VariableInitializers = append(result.VariableInitializers, variables...)
			result.CallInitializers = append(result.CallInitializers, calls...)
			if initFunction != "" {
				result.InitFunctions = append(result.InitFunctions, initFunction)
			}
			identity := key + "@" + hash
			result.Locations[identity] = append(result.Locations[identity], source.Path)
		}
	}
	sort.Slice(result.Declarations, func(i, j int) bool {
		if result.Declarations[i].Key != result.Declarations[j].Key {
			return result.Declarations[i].Key < result.Declarations[j].Key
		}
		if result.Declarations[i].Hash != result.Declarations[j].Hash {
			return result.Declarations[i].Hash < result.Declarations[j].Hash
		}
		return result.Declarations[i].Constraint < result.Declarations[j].Constraint
	})
	sort.Strings(result.ExportedNames)
	result.CommentTokens = records(commentCounts)
	result.CompilerDirectives = records(directiveCounts)
	return result
}

func inspectDeclaration(node ast.Decl, text string) (string, []string, []string, []string, string) {
	var exported []string
	var variables []string
	var calls []string
	switch item := node.(type) {
	case *ast.FuncDecl:
		key := item.Name.Name
		if item.Recv != nil {
			key = receiverName(item.Recv.List[0].Type) + "." + key
		}
		if ast.IsExported(item.Name.Name) {
			exported = append(exported, key)
		}
		initFunction := ""
		if item.Recv == nil && item.Name.Name == "init" {
			initFunction = "func:init@" + digest(text)
		}
		return key, exported, variables, calls, initFunction
	case *ast.GenDecl:
		var names []string
		for _, raw := range item.Specs {
			switch spec := raw.(type) {
			case *ast.TypeSpec:
				names = append(names, spec.Name.Name)
				if ast.IsExported(spec.Name.Name) {
					exported = append(exported, spec.Name.Name)
				}
			case *ast.ValueSpec:
				var specNames []string
				for _, name := range spec.Names {
					names = append(names, name.Name)
					specNames = append(specNames, name.Name)
					if ast.IsExported(name.Name) {
						exported = append(exported, name.Name)
					}
				}
				if item.Tok == token.VAR && len(spec.Values) > 0 {
					identity := "var:" + strings.Join(specNames, ",") + "@" + nodeHash(spec)
					variables = append(variables, identity)
					for _, value := range spec.Values {
						if hasCall(value) {
							calls = append(calls, identity)
							break
						}
					}
				}
			}
		}
		return item.Tok.String() + ":" + strings.Join(names, ","), exported, variables, calls, ""
	default:
		panic(fmt.Sprintf("unsupported declaration %T", node))
	}
}

func compareSnapshots(base, target snapshot) error {
	checks := []struct {
		name string
		base any
		want any
	}{
		{"declarations", base.Declarations, target.Declarations},
		{"comment tokens", base.CommentTokens, target.CommentTokens},
		{"exported names", base.ExportedNames, target.ExportedNames},
		{"init function order", base.InitFunctions, target.InitFunctions},
		{"compiler directives", base.CompilerDirectives, target.CompilerDirectives},
	}
	for _, check := range checks {
		if !equal(check.base, check.want) {
			return fmt.Errorf("%s changed", check.name)
		}
	}
	baseCalls := append([]string(nil), base.CallInitializers...)
	targetCalls := append([]string(nil), target.CallInitializers...)
	sort.Strings(baseCalls)
	sort.Strings(targetCalls)
	if !equal(baseCalls, targetCalls) {
		return fmt.Errorf("call-bearing initializer set changed")
	}
	return nil
}

func loadFiles(ref string, excluded map[string]bool) []fileContent {
	var paths []string
	if ref == "WORKTREE" {
		matches, err := filepath.Glob(filepath.Join(uiRoot, "*.go"))
		must(err)
		for _, path := range matches {
			paths = append(paths, filepath.ToSlash(path))
		}
	} else {
		output := git("ls-tree", "-r", "--name-only", ref, "--", uiRoot)
		for _, path := range strings.Split(strings.TrimSpace(output), "\n") {
			if path != "" && strings.HasSuffix(path, ".go") {
				paths = append(paths, path)
			}
		}
	}
	sort.Strings(paths)
	result := make([]fileContent, 0, len(paths))
	for _, path := range paths {
		if excluded[path] || excluded[filepath.Base(path)] {
			continue
		}
		var data []byte
		if ref == "WORKTREE" {
			var err error
			data, err = os.ReadFile(path)
			must(err)
		} else {
			data = []byte(git("show", ref+":"+path))
		}
		result = append(result, fileContent{Path: path, Data: data})
	}
	return result
}

func buildConstraint(file *ast.File) string {
	var lines []string
	for _, group := range file.Comments {
		if group.End() >= file.Package {
			continue
		}
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, "//go:build ") || strings.HasPrefix(comment.Text, "// +build ") {
				lines = append(lines, comment.Text)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func hasCall(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found {
			return false
		}
		switch node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			found = true
			return false
		}
		return true
	})
	return found
}

func receiverName(expr ast.Expr) string {
	if pointer, ok := expr.(*ast.StarExpr); ok {
		expr = pointer.X
	}
	name, ok := expr.(*ast.Ident)
	if !ok {
		fatalf("unsupported receiver %T", expr)
	}
	return name.Name
}

func nodeHash(node ast.Node) string {
	var output bytes.Buffer
	must(format.Node(&output, token.NewFileSet(), node))
	return digest(output.String())
}

func records(values map[string]int) []count {
	result := make([]count, 0, len(values))
	for text, amount := range values {
		result = append(result, count{Text: text, Count: amount})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Text < result[j].Text })
	return result
}

func writeMoveReport(path, baseRef, targetRef string, base, target snapshot) {
	report := moveReport{Schema: 1, Base: baseRef, Target: targetRef}
	for identity, fromFiles := range base.Locations {
		toFiles := target.Locations[identity]
		sort.Strings(fromFiles)
		sort.Strings(toFiles)
		if equal(fromFiles, toFiles) {
			continue
		}
		parts := strings.SplitN(identity, "@", 2)
		for index := 0; index < len(fromFiles) && index < len(toFiles); index++ {
			report.Moves = append(report.Moves, move{Key: parts[0], Hash: parts[1], From: fromFiles[index], To: toFiles[index]})
		}
	}
	sort.Slice(report.Moves, func(i, j int) bool {
		if report.Moves[i].From != report.Moves[j].From {
			return report.Moves[i].From < report.Moves[j].From
		}
		if report.Moves[i].To != report.Moves[j].To {
			return report.Moves[i].To < report.Moves[j].To
		}
		return report.Moves[i].Key < report.Moves[j].Key
	})
	data, err := json.MarshalIndent(report, "", "  ")
	must(err)
	data = append(data, '\n')
	must(os.WriteFile(path, data, 0o644))
}

func parseExcludes(raw string) map[string]bool {
	result := make(map[string]bool)
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			result[filepath.ToSlash(value)] = true
		}
	}
	return result
}

func git(args ...string) string {
	command := exec.Command("git", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func equal(left, right any) bool {
	a, err := json.Marshal(left)
	must(err)
	b, err := json.Marshal(right)
	must(err)
	return bytes.Equal(a, b)
}

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func total(values []count) int {
	result := 0
	for _, value := range values {
		result += value.Count
	}
	return result
}

func must(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
