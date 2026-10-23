package ui

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

func loadPathSuggestions(m *Model, target pathcomplete.Target, typed string) {
	switch target {
	case pathcomplete.Form:
		m.form().SetDir(typed)
	case pathcomplete.Group:
		m.groupSurface().form.SetDir(typed)
	case pathcomplete.Rename:
		openRename(m).SetDir(typed)
	}
	msg := m.requestPathSuggestions(target, typed)().(pathcomplete.Msg)
	m.completer(target).Handle(pathCompleter{m}, msg)
}

type blockedPathSuggestionReader struct {
	started     chan struct{}
	release     chan struct{}
	once        sync.Once
	suggestions []string
}

func (r *blockedPathSuggestionReader) Complete(string) []string {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.suggestions
}

func TestPathSuggestionsDeferScanAndRejectStaleInput(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	m.form().SetDir("/first")
	reader := &blockedPathSuggestionReader{
		started: make(chan struct{}), release: make(chan struct{}),
		suggestions: []string{"/first-result"},
	}
	cmd := m.form().complete.Request(pathcomplete.Form, m.form().Dir(), reader)
	select {
	case <-reader.started:
		t.Fatal("path scan ran on the update path")
	default:
	}
	completed := make(chan pathcomplete.Msg, 1)
	go func() { completed <- cmd().(pathcomplete.Msg) }()
	<-reader.started
	m.form().SetDir("/newer")
	m.form().complete.Request(pathcomplete.Form, m.form().Dir(), pathcomplete.SystemReader{})
	close(reader.release)
	m.form().complete.Handle(pathCompleter{m}, <-completed)
	if len(m.form().complete.Suggestions) != 0 {
		t.Fatalf("stale scan replaced newer input: %v", m.form().complete.Suggestions)
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

func TestApplyPathSuggestionFillsDirField(t *testing.T) {
	root := setupCompletionDir(t)
	m := &Model{}
	pushForm(m)
	loadPathSuggestions(m, pathcomplete.Form, filepath.Join(root, "al"))
	if !m.form().complete.Active() {
		t.Fatal("expected suggestions")
	}
	m.applyPathSuggestion()
	want := filepath.Join(root, "alpha") + string(filepath.Separator)
	if m.form().Dir() != want {
		t.Fatalf("dir = %q want %q", m.form().Dir(), want)
	}
	if m.form().DirAuto() {
		t.Fatal("dirAuto should be cleared after completion")
	}
}

func TestPathSuggestionsExitToAdjacentFormFields(t *testing.T) {
	t.Parallel()
	root := setupCompletionDir(t)
	m := buildModel(t)
	m.openForm()
	m.form().FocusField(m.spawnHost(), spawn.FieldDir)
	loadPathSuggestions(m, pathcomplete.Form, filepath.Join(root, "a"))

	m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
	if !m.form().complete.Chosen || m.form().complete.Index != 0 {
		t.Fatalf("first down should select the first suggestion, chosen=%v index=%d",
			m.form().complete.Chosen, m.form().complete.Index)
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.form().complete.Index != 1 {
		t.Fatalf("second down should select the second suggestion, index=%d", m.form().complete.Index)
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.form().Focused() != spawn.FieldWorktree {
		t.Fatalf("down past the last suggestion should focus worktree, focus=%d", m.form().Focused())
	}

	m.form().FocusStep(m.spawnHost(), -1)
	loadPathSuggestions(m, pathcomplete.Form, filepath.Join(root, "a"))
	m.form().complete.Chosen = true
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.form().Focused() != spawn.FieldTool {
		t.Fatalf("up past the first suggestion should focus tool, focus=%d", m.form().Focused())
	}
}

func TestGroupPickerExitsToAdjacentFields(t *testing.T) {
	t.Parallel()
	m := buildModel(t)

	m.openGroupForm()
	m.pressGroupKey(tea.KeyMsg{Type: tea.KeyTab})
	m.groupSurface().parent = 0
	m.pressGroupKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.groupSurface().form.Focus() != uidialogs.GroupFormNameField {
		t.Fatalf("up past first parent should focus name, focus=%d", m.groupSurface().form.Focus())
	}

	m.openGroupForm()
	m.pressGroupKey(tea.KeyMsg{Type: tea.KeyTab})
	m.groupSurface().parent = len(m.groupSurface().parents) - 1
	m.pressGroupKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.groupSurface().form.Focus() != uidialogs.GroupFormPathField {
		t.Fatalf("down past last parent should focus path, focus=%d", m.groupSurface().form.Focus())
	}

	m.openForm()
	m.form().FocusField(m.spawnHost(), spawn.FieldGroup)
	picker(m).GroupIndex = len(picker(m).Groups) - 1
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.form().Focused() != spawn.FieldName {
		t.Fatalf("down past last group should wrap to name, focus=%d", m.form().Focused())
	}
}

func TestStandaloneGroupPickerWrapsWhenThereAreNoAdjacentFields(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	s := newMoveSurface(uidialogs.MoveSession("s"), groupPicker{Groups: []groupOption{{Path: ""}, {Path: "alpha"}, {Path: "beta"}}})
	m.push(s)

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if s.GroupIndex != 2 {
		t.Fatalf("up past first group should wrap to last, index=%d", s.GroupIndex)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if s.GroupIndex != 0 {
		t.Fatalf("down past last group should wrap to first, index=%d", s.GroupIndex)
	}
}

func TestRenamePathSuggestionsExitToName(t *testing.T) {
	t.Parallel()
	root := setupCompletionDir(t)
	m := buildModel(t)
	m.push(newRenameSurface(uidialogs.RenameGroup("", "", 0), ""))
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	loadPathSuggestions(m, pathcomplete.Rename, filepath.Join(root, "a"))
	openRename(m).complete.Chosen = true
	openRename(m).complete.Index = len(openRename(m).complete.Suggestions) - 1

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if openRename(m).Focus() != 2 {
		t.Fatalf("down past last suggestion should move to worktree, focus=%d", openRename(m).Focus())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if openRename(m).Focus() != 3 {
		t.Fatalf("down past worktree should move to base, focus=%d", openRename(m).Focus())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if openRename(m).Focus() != 0 {
		t.Fatalf("down past base should wrap to name, focus=%d", openRename(m).Focus())
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
	if m.groupSurface().form.Dir() != parentPath {
		t.Fatalf("cursor on group should inherit its path, got %q want %q", m.groupSurface().form.Dir(), parentPath)
	}
	pickGroup(t, m, "")
	m.groupSurface().stepParent(0)
	if m.groupSurface().form.Dir() != "" {
		t.Fatalf("root parent should clear auto path, got %q", m.groupSurface().form.Dir())
	}

	m.groupSurface().form.TakePath("/custom")
	pickGroup(t, m, "")
	m.groupSurface().stepParent(0)
	if m.groupSurface().form.Dir() != "/custom" {
		t.Fatalf("manual path should survive parent change, got %q", m.groupSurface().form.Dir())
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
	m.groupSurface().form.SetName("relgrp")
	m.groupSurface().form.SetDir("sub")
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

// A scan the form's previous opening asked for never fills the reopened
// form, even over the same text.
func TestReopenedFormIgnoresTheLastOpeningsScan(t *testing.T) {
	t.Parallel()
	root := setupCompletionDir(t)
	m := buildModel(t)
	m.openForm()
	typed := filepath.Join(root, "a")
	m.form().SetDir(typed)
	stale := m.requestPathSuggestions(pathcomplete.Form, typed)().(pathcomplete.Msg)
	m.base = baseList
	m.clearSurfaces()
	m.openForm()
	m.form().SetDir(typed)
	m.requestPathSuggestions(pathcomplete.Form, typed)
	m.completer(pathcomplete.Form).Handle(pathCompleter{m}, stale)
	if m.form().complete.Active() {
		t.Fatalf("reopened form took the last opening's scan: %v", m.form().complete.Suggestions)
	}
	// Only the reopened form is up: a form scan that reached another dialog
	// would have had to push it.
	if len(m.surfaces) != 1 {
		t.Fatalf("a form scan filled another dialog's completer: %d surfaces", len(m.surfaces))
	}
}
