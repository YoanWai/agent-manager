package ui

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

func loadPathSuggestions(m *Model, target pathSuggestionTarget, typed string) {
	switch target {
	case pathSuggestionForm:
		m.form.SetDir(typed)
	case pathSuggestionGroup:
		m.groupForm.SetDir(typed)
	case pathSuggestionRename:
		m.rename.SetDir(typed)
	}
	msg := m.requestPathSuggestions(target, typed)().(pathSuggestionsMsg)
	m.completer(target).handle(m, msg)
}

type blockedPathSuggestionReader struct {
	started     chan struct{}
	release     chan struct{}
	once        sync.Once
	suggestions []string
}

func (r *blockedPathSuggestionReader) complete(string) []string {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.suggestions
}

func TestPathSuggestionsDeferScanAndRejectStaleInput(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	m.form.SetDir("/first")
	reader := &blockedPathSuggestionReader{
		started: make(chan struct{}), release: make(chan struct{}),
		suggestions: []string{"/first-result"},
	}
	cmd := m.form.paths.request(pathSuggestionForm, m.form.Dir(), reader)
	select {
	case <-reader.started:
		t.Fatal("path scan ran on the update path")
	default:
	}
	completed := make(chan pathSuggestionsMsg, 1)
	go func() { completed <- cmd().(pathSuggestionsMsg) }()
	<-reader.started
	m.form.SetDir("/newer")
	m.form.paths.request(pathSuggestionForm, m.form.Dir(), systemPathSuggestionReader{})
	close(reader.release)
	m.form.paths.handle(m, <-completed)
	if len(m.form.paths.suggestions) != 0 {
		t.Fatalf("stale scan replaced newer input: %v", m.form.paths.suggestions)
	}
}

func setupCompletionDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"alpha", "amber", "beta", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "afile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCompleteDirsMatchesPrefix(t *testing.T) {
	root := setupCompletionDir(t)
	got := completeDirs(filepath.Join(root, "a"))
	want := []string{filepath.Join(root, "alpha"), filepath.Join(root, "amber")}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestCompleteDirsTrailingSlashListsChildren(t *testing.T) {
	root := setupCompletionDir(t)
	got := completeDirs(root + "/")
	if len(got) != 3 {
		t.Fatalf("expected 3 visible dirs, got %v", got)
	}
	for _, path := range got {
		if filepath.Base(path) == ".hidden" || filepath.Base(path) == "afile" {
			t.Fatalf("unexpected entry %s", path)
		}
	}
}

func TestCompleteDirsHiddenNeedsDotPrefix(t *testing.T) {
	root := setupCompletionDir(t)
	got := completeDirs(filepath.Join(root, ".h"))
	if len(got) != 1 || filepath.Base(got[0]) != ".hidden" {
		t.Fatalf("got %v", got)
	}
}

func TestCompleteDirsNoSlashNoSuggestions(t *testing.T) {
	if got := completeDirs("relative"); got != nil {
		t.Fatalf("got %v", got)
	}
	if got := completeDirs(""); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestApplyPathSuggestionFillsDirField(t *testing.T) {
	root := setupCompletionDir(t)
	m := &Model{
		mode: modeForm,
	}
	loadPathSuggestions(m, pathSuggestionForm, filepath.Join(root, "al"))
	if !m.form.paths.active() {
		t.Fatal("expected suggestions")
	}
	m.applyPathSuggestion()
	want := filepath.Join(root, "alpha") + string(filepath.Separator)
	if m.form.Dir() != want {
		t.Fatalf("dir = %q want %q", m.form.Dir(), want)
	}
	if m.form.DirAuto() {
		t.Fatal("dirAuto should be cleared after completion")
	}
}

func TestPathSuggestionsExitToAdjacentFormFields(t *testing.T) {
	t.Parallel()
	root := setupCompletionDir(t)
	m := buildModel(t)
	m.openForm()
	m.form.FocusField(m.spawnHost(), spawn.FieldDir)
	loadPathSuggestions(m, pathSuggestionForm, filepath.Join(root, "a"))

	m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
	if !m.form.paths.chosen || m.form.paths.index != 0 {
		t.Fatalf("first down should select the first suggestion, chosen=%v index=%d",
			m.form.paths.chosen, m.form.paths.index)
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.form.paths.index != 1 {
		t.Fatalf("second down should select the second suggestion, index=%d", m.form.paths.index)
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.form.Focused() != spawn.FieldWorktree {
		t.Fatalf("down past the last suggestion should focus worktree, focus=%d", m.form.Focused())
	}

	m.form.FocusStep(m.spawnHost(), -1)
	loadPathSuggestions(m, pathSuggestionForm, filepath.Join(root, "a"))
	m.form.paths.chosen = true
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.form.Focused() != spawn.FieldTool {
		t.Fatalf("up past the first suggestion should focus tool, focus=%d", m.form.Focused())
	}
}

func TestGroupPickerExitsToAdjacentFields(t *testing.T) {
	t.Parallel()
	m := buildModel(t)

	m.openGroupForm()
	m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyTab})
	m.form.groupIndex = 0
	m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.groupForm.Focus() != uidialogs.GroupFormNameField {
		t.Fatalf("up past first parent should focus name, focus=%d", m.groupForm.Focus())
	}

	m.openGroupForm()
	m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyTab})
	m.form.groupIndex = len(m.form.groups) - 1
	m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.groupForm.Focus() != uidialogs.GroupFormPathField {
		t.Fatalf("down past last parent should focus path, focus=%d", m.groupForm.Focus())
	}

	m.openForm()
	m.form.FocusField(m.spawnHost(), spawn.FieldGroup)
	m.form.groupIndex = len(m.form.groups) - 1
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.form.Focused() != spawn.FieldName {
		t.Fatalf("down past last group should wrap to name, focus=%d", m.form.Focused())
	}
}

func TestStandaloneGroupPickerWrapsWhenThereAreNoAdjacentFields(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.form.groups = []groupOption{{path: ""}, {path: "alpha"}, {path: "beta"}}
	m.mode = modeMove

	m.form.groupIndex = 0
	m.handleMoveKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.form.groupIndex != 2 {
		t.Fatalf("up past first group should wrap to last, index=%d", m.form.groupIndex)
	}

	m.handleMoveKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.form.groupIndex != 0 {
		t.Fatalf("down past last group should wrap to first, index=%d", m.form.groupIndex)
	}
}

func TestRenamePathSuggestionsExitToName(t *testing.T) {
	t.Parallel()
	root := setupCompletionDir(t)
	m := buildModel(t)
	m.mode = modeRename
	m.rename.Rename = uidialogs.RenameGroup("", "", 0)
	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyTab})
	loadPathSuggestions(m, pathSuggestionRename, filepath.Join(root, "a"))
	m.rename.paths.chosen = true
	m.rename.paths.index = len(m.rename.paths.suggestions) - 1

	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.rename.Focus() != 2 {
		t.Fatalf("down past last suggestion should move to worktree, focus=%d", m.rename.Focus())
	}
	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.rename.Focus() != 3 {
		t.Fatalf("down past worktree should move to base, focus=%d", m.rename.Focus())
	}
	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.rename.Focus() != 0 {
		t.Fatalf("down past base should wrap to name, focus=%d", m.rename.Focus())
	}
}

func TestGroupFormInheritsParentPath(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	parentPath := t.TempDir()
	if err := m.services.store.CreateGroup("projects", parentPath); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	// The cursor starts on root, which has no stored path to inherit.
	m.selectGroupRow(t, "projects")
	m.openGroupForm()
	if m.groupForm.Dir() != parentPath {
		t.Fatalf("cursor on group should inherit its path, got %q want %q", m.groupForm.Dir(), parentPath)
	}
	pickGroup(t, m, "")
	m.moveGroupCursor(0)
	if m.groupForm.Dir() != "" {
		t.Fatalf("root parent should clear auto path, got %q", m.groupForm.Dir())
	}

	m.groupForm.TakePath("/custom")
	pickGroup(t, m, "")
	m.moveGroupCursor(0)
	if m.groupForm.Dir() != "/custom" {
		t.Fatalf("manual path should survive parent change, got %q", m.groupForm.Dir())
	}
}

func TestAncestorGroupPathWalksUp(t *testing.T) {
	root := t.TempDir()
	m := &Model{
		workspace: workspace{
			groupPaths: map[string]string{"projects": root},
		},
	}
	if got := m.capturedAncestorGroupDir("projects/api/auth"); got != root {
		t.Fatalf("got %q want %q", got, root)
	}
	if got := m.capturedAncestorGroupDir("other"); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestRelativePathsStoredAbsolute(t *testing.T) {
	m := buildModel(t)
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(base)

	m.openGroupForm()
	m.groupForm.SetName("relgrp")
	m.groupForm.SetDir("sub")
	_, cmd := m.submitGroupForm()
	if m.errBar.text != "" {
		t.Fatalf("group form should submit, err=%q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if g.Name == "relgrp" && !filepath.IsAbs(g.Path) {
			t.Fatalf("group path stored relative: %q", g.Path)
		}
	}
}

type fakePathCompleteHost struct {
	value string
	open  bool
}

func (h fakePathCompleteHost) pathSuggestionField(pathSuggestionTarget) (string, bool) {
	return h.value, h.open
}

type fixedPathSuggestionReader []string

func (r fixedPathSuggestionReader) complete(string) []string { return r }

// The completer takes a read's answer only for the latest request, and only
// while its field still holds what was typed.
func TestPathCompleteThroughAFakeHost(t *testing.T) {
	var pc pathComplete
	reader := fixedPathSuggestionReader{"/work/alpha", "/work/amber"}
	stale := pc.request(pathSuggestionGroup, "/work/a", reader)().(pathSuggestionsMsg)
	fresh := pc.request(pathSuggestionGroup, "/work/a", reader)().(pathSuggestionsMsg)
	pc.handle(fakePathCompleteHost{value: "/work/a", open: true}, stale)
	if pc.active() {
		t.Fatal("a superseded read filled the list")
	}
	pc.handle(fakePathCompleteHost{value: "/work/a", open: false}, fresh)
	if pc.active() {
		t.Fatal("a read for a closed field filled the list")
	}
	pc.handle(fakePathCompleteHost{value: "/work/al", open: true}, fresh)
	if pc.active() {
		t.Fatal("a read for text since edited filled the list")
	}
	pc.handle(fakePathCompleteHost{value: "/work/a", open: true}, fresh)
	if !pc.move(1) || pc.selected() != "/work/alpha" {
		t.Fatalf("suggestions = %v", pc.suggestions)
	}
}

// A scan the form's previous opening asked for never fills the reopened
// form, even over the same text.
func TestReopenedFormIgnoresTheLastOpeningsScan(t *testing.T) {
	t.Parallel()
	root := setupCompletionDir(t)
	m := buildModel(t)
	m.openForm()
	typed := filepath.Join(root, "a")
	m.form.SetDir(typed)
	stale := m.requestPathSuggestions(pathSuggestionForm, typed)().(pathSuggestionsMsg)
	m.mode = modeList
	m.openForm()
	m.form.SetDir(typed)
	m.requestPathSuggestions(pathSuggestionForm, typed)
	m.completer(pathSuggestionForm).handle(m, stale)
	if m.form.paths.active() {
		t.Fatalf("reopened form took the last opening's scan: %v", m.form.paths.suggestions)
	}
	if m.groupForm.paths.active() || m.rename.paths.active() {
		t.Fatal("a form scan filled another dialog's completer")
	}
}
