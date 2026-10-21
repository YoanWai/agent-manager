package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type effectPair struct {
	request effectRequest
	result  effectResult
}

var effectPairs = []effectPair{
	{inputRequest{}, inputEffectResult{}},
	{quickSendRequest{}, quickSendResult{}},
	{noticeDismissRequest{}, noticeDismissResult{}},
	{splitSaveRequest{}, splitSaveResult{}},
	{choiceSaveRequest{}, choiceSaveResult{}},
	{installStartRequest{}, installStartResult{}},
	{installSettleRequest{}, installSettleResult{}},
	{lifecycleRequest{}, lifecycleEffectResult{}},
	{afterTurnRequest{}, afterTurnResult{}},
	{railRequest{}, railEffectResult{}},
	{forkRequest{}, forkEffectResult{}},
	{geometryRequest{}, geometryEffectResult{}},
	{spawnRequest{}, spawnEffectResult{}},
	{groupRequest{}, groupEffectResult{}},
	{renameRequest{}, renameEffectResult{}},
	{moveDialogClose{}, moveDialogCloseResult{}},
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
// reported, by the default branch of handleEffectCompleted.
var resultsAppliedByDefault = []string{"choiceSaveResult"}

func TestEveryEffectRequestIsKnownToAllThreeRegistries(t *testing.T) {
	declared := parseEffectDeclarations(t)

	listedRequests := typeNamesOf(effectPairs, func(pair effectPair) any { return pair.request })
	if markers := declared.receiversOf("effectRequest"); !slices.Equal(markers, listedRequests) {
		t.Fatalf("effectRequest markers %q differ from effectPairs %q; add the new request and its result to effectPairs", markers, listedRequests)
	}
	listedResults := typeNamesOf(effectPairs, func(pair effectPair) any { return pair.result })
	if markers := declared.receiversOf("effectResult"); !slices.Equal(markers, listedResults) {
		t.Fatalf("effectResult markers %q differ from effectPairs %q", markers, listedResults)
	}

	nameCases := declared.typeSwitchCases(t, "effectName", "request")
	captureCases := declared.typeSwitchCases(t, "captureEffect", "request")
	completedCases := declared.typeSwitchCases(t, "handleEffectCompleted", "msg.result")

	for _, pair := range effectPairs {
		requestName := reflect.TypeOf(pair.request).Name()
		if effectName(pair.request) == "" {
			t.Errorf("effectName(%s) is empty", requestName)
		}
		if !nameCases[requestName] {
			t.Errorf("effectName has no case for %s", requestName)
		}
		if !captureCases[requestName] {
			t.Errorf("captureEffect has no case for %s", requestName)
		}
		resultName := reflect.TypeOf(pair.result).Name()
		appliedByDefault := slices.Contains(resultsAppliedByDefault, resultName)
		if appliedByDefault && completedCases[resultName] {
			t.Errorf("handleEffectCompleted now applies %s; drop it from resultsAppliedByDefault", resultName)
		}
		if !appliedByDefault && !completedCases[resultName] {
			t.Errorf("handleEffectCompleted has no case for %s, the result of %s", resultName, requestName)
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

func (d effectDeclarations) function(name string) *ast.FuncDecl {
	for _, file := range d.files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Name.Name == name {
				return function
			}
		}
	}
	return nil
}

func (d effectDeclarations) typeSwitchCases(t *testing.T, functionName, subject string) map[string]bool {
	t.Helper()
	function := d.function(functionName)
	if function == nil {
		t.Fatalf("no function %s in package ui", functionName)
	}
	cases := map[string]bool{}
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		typeSwitch, ok := node.(*ast.TypeSwitchStmt)
		if !ok || types(typeSwitchSubject(typeSwitch)) != subject {
			return true
		}
		found = true
		for _, clause := range typeSwitch.Body.List {
			for _, caseType := range clause.(*ast.CaseClause).List {
				cases[types(caseType)] = true
			}
		}
		return true
	})
	if !found {
		t.Fatalf("%s has no type switch on %s", functionName, subject)
	}
	return cases
}

func typeSwitchSubject(typeSwitch *ast.TypeSwitchStmt) ast.Expr {
	switch assign := typeSwitch.Assign.(type) {
	case *ast.AssignStmt:
		return assign.Rhs[0].(*ast.TypeAssertExpr).X
	case *ast.ExprStmt:
		return assign.X.(*ast.TypeAssertExpr).X
	}
	return nil
}
