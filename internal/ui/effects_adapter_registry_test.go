package ui

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type effectPair struct {
	request effectRequest
	result  uieffects.Result
}

var effectPairs = []effectPair{
	{inputRequest{}, inputEffectResult{}},
	{quickSendRequest{}, quickSendResult{}},
	{noticeDismissRequest{}, noticeDismissResult{}},
	{splitSaveRequest{}, splitSaveResult{}},
	{choiceSaveRequest{}, choiceSaveResult{}},
	{lifecycleRequest{}, lifecycleEffectResult{}},
	{afterTurnRequest{}, afterTurnResult{}},
	{railRequest{}, railEffectResult{}},
	{geometryRequest{}, geometryEffectResult{}},
	{spawnRequest{}, spawnEffectResult{}},
	{groupRequest{}, groupEffectResult{}},
	{settingsRequest{}, settingsEffectResult{}},
	{savedPromptRequest{}, savedPromptResult{}},
	{reviewEffectRequest{}, reviewEffectResult{}},
	{keysRequest{}, keysEffectResult{}},
	{focusRequest{}, focusEffectResult{}},
	{ackRequest{}, ackEffectResult{}},
	{detachRequest{}, detachEffectResult{}},
	{attachRequest{}, attachEffectResult{}},
}

// A choice save carries nothing back to the model, so only its error is
// reported, by handleEffectCompleted itself.
var resultsAppliedByDefault = []string{"choiceSaveResult"}

func TestEveryEffectRequestIsPairedWithAResultThatAppliesIt(t *testing.T) {
	declared := parseEffectDeclarations(t)

	listedRequests := typeNamesOf(effectPairs, func(pair effectPair) any { return pair.request })
	if markers := declared.receiversOf("EffectName"); !slices.Equal(markers, listedRequests) {
		t.Fatalf("root requests %q differ from effectPairs %q; add the new request and its result to effectPairs", markers, listedRequests)
	}
	listedResults := typeNamesOf(effectPairs, func(pair effectPair) any { return pair.result })
	if markers := declared.receiversOf("EffectResult"); !slices.Equal(markers, listedResults) {
		t.Fatalf("root results %q differ from effectPairs %q", markers, listedResults)
	}

	for _, pair := range effectPairs {
		requestName := reflect.TypeOf(pair.request).Name()
		if pair.request.EffectName() == "" {
			t.Errorf("%s names itself with nothing", requestName)
		}
		resultName := reflect.TypeOf(pair.result).Name()
		_, applies := pair.result.(effectResult)
		appliedByDefault := slices.Contains(resultsAppliedByDefault, resultName)
		if appliedByDefault && applies {
			t.Errorf("%s now applies itself; drop it from resultsAppliedByDefault", resultName)
		}
		if !appliedByDefault && !applies {
			t.Errorf("%s, the result of %s, has no apply", resultName, requestName)
		}
	}
}

func typeNamesOf(pairs []effectPair, pick func(effectPair) any) []string {
	names := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		names = append(names, reflect.TypeOf(pick(pair)).Name())
	}
	slices.Sort(names)
	return names
}

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

func (d effectDeclarations) receiversOf(markerMethod string) []string {
	var receivers []string
	for _, file := range d.files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Name.Name != markerMethod {
				continue
			}
			receivers = append(receivers, types(function.Recv.List[0].Type))
		}
	}
	slices.Sort(receivers)
	return receivers
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
