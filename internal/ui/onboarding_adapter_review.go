package ui

import (
	"strings"

	diffmodel "github.com/YoanWai/agent-manager/internal/diff/model"
	gitvalue "github.com/YoanWai/agent-manager/internal/git/value"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
)

const (
	sampleRedirectBefore = `function next(req) {
  const to = req.to;
  log(to);
  return to ?? "/";
}
`
	sampleRedirectAfter = `function next(req) {
  const to = req.to;
  return safe(to) ?? "/";
}

function safe(to) {
  return to?.[0] == "/"
    ? to : null;
}
`
	sampleRedirectTest = `test("lands where it began", () => {
  expect(next(at("/docs"))).toBe("/docs");
});
`
	sampleRepo = "/work/web"
	// sampleCommentLine is the new-side line the sample comment sits on.
	sampleCommentLine = 3
	// previewFileWidth is the least the sample Review's file list takes:
	// narrower than Review's own, so the code keeps its lines in a small
	// window.
	previewFileWidth = 18
)

// previewReview is Review on a sample diff of the login agent's work,
// drawn by Review's own renderer, with comment on one line when it is set.
func (m *Model) previewReview(width, height int, comment string) []string {
	review := uireview.New(false)
	target := uireview.Target{ID: previewLogin, Name: "login redirect"}
	scope := gitvalue.ScopeBranch
	request := review.Open(target, scope, "")
	source := gitvalue.ChangedFile{Path: "src/auth/redirect.ts", Status: gitvalue.Modified}
	test := gitvalue.ChangedFile{Path: "src/auth/redirect.test.ts", Status: gitvalue.Added}
	file := diffmodel.BuildFile([]byte(sampleRedirectBefore), []byte(sampleRedirectAfter), source, gitvalue.FileStat{Adds: 6, Dels: 2})
	file.HasStat = true
	testFile := diffmodel.BuildFile(nil, []byte(sampleRedirectTest), test, gitvalue.FileStat{Adds: 3})
	testFile.HasStat = true
	set := diffmodel.Set{
		Repo:     gitvalue.Repo{Root: sampleRepo, Branch: "am/login-redirect"},
		Scope:    scope,
		BaseDesc: "main",
		Files: []diffmodel.FileDiff{
			{File: source, Stat: file.Stat, HasStat: true},
			testFile,
		},
	}
	saved := uireview.SavedState{}
	if comment != "" {
		saved.Comments = []uireview.Comment{{ID: "preview-comment", File: source.Path, Line: sampleCommentLine,
			Text: comment, ContentHash: uireview.ContentHash(&file), Scope: scope.String()}}
	}
	review.ApplyLoad(uireview.LoadResult{TargetID: target.ID, Scope: scope, Generation: request.Generation, Set: set,
		RepoRoots: []string{sampleRepo}, RepoRoot: sampleRepo, Saved: saved, SavedLoaded: true})
	review.ApplyFile(uireview.FileResult{TargetID: target.ID, Scope: scope, Generation: request.Generation,
		RepoRoot: sampleRepo, Index: 0, Path: source.Path, File: file})

	ctx := m.reviewRenderContext()
	bodyHeight := max(height-3, 4)
	fileWidth := max(previewFileWidth, width/4)
	codeWidth := width - fileWidth - 2
	review.PrepareLayout(ctx, max(codeWidth-2*contentGutter, 1), bodyHeight-2)
	files := append([]string{"", strings.Repeat(" ", railInset) + subtleStyle().Render("files")},
		indentLines(splitLines(review.FileList(ctx, fileWidth-2*railGutter, bodyHeight-2).Text), railInset)...)
	code := append([]string{"", "  " + subtleStyle().Render(escapeControlsInline(review.CodeTitle()))},
		indentLines(splitLines(review.Code(ctx, codeWidth-2*contentGutter, bodyHeight-2).Text), contentGutter)...)
	edge := make([]string, bodyHeight)
	for i := range edge {
		edge[i] = railEdgeCell(panelHex())
	}
	out := []string{
		paint(review.Header(ctx, target.Name, width), width, backdropHex()),
		m.boundedRuleRow(fileWidth+1, width, "▀"),
	}
	out = append(out, joinColumns(edge, paintRows(files, fileWidth-1, bodyHeight, panelHex()), m.vruleColumn(bodyHeight),
		m.bleedColumn(bodyHeight), paintRows(code, codeWidth, bodyHeight, backdropHex()))...)
	return append(out, m.boundedRuleRow(fileWidth+1, width, "▄"))
}
