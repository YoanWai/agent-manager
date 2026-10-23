package ui

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
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
	result  uieffects.Result
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
	{connectionRequest{}, connectionEffectResult{}},
	{remoteRequest{}, remoteEffectResult{}},
}

// A choice save carries nothing back to the model, so only its error is
// reported, by handleEffectCompleted itself.
var resultsAppliedByDefault = []string{"choiceSaveResult"}

func TestEveryEffectRequestIsPairedWithAResultThatAppliesIt(t *testing.T) {
	declared := parseEffectDeclarations(t)

	listedRequests := typeNamesOf(effectPairs, func(pair effectPair) any { return pair.request })
	if markers := declared.receiversOf("EffectRequest"); !slices.Equal(markers, listedRequests) {
		t.Fatalf("EffectRequest markers %q differ from effectPairs %q; add the new request and its result to effectPairs", markers, listedRequests)
	}
	listedResults := typeNamesOf(effectPairs, func(pair effectPair) any { return pair.result })
	if markers := declared.receiversOf("EffectResult"); !slices.Equal(markers, listedResults) {
		t.Fatalf("EffectResult markers %q differ from effectPairs %q", markers, listedResults)
	}

	for _, pair := range effectPairs {
		requestName := reflect.TypeOf(pair.request).Name()
		if pair.request.effectName() == "" {
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
