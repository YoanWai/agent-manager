package ui

import (
	"os"
	"path/filepath"
	"testing"
)

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
	m := &Model{mode: modeForm}
	m.form.dir = textField("", 400)
	m.pathSugg.recompute(filepath.Join(root, "al"))
	if !m.pathSugg.active() {
		t.Fatal("expected suggestions")
	}
	m.applyPathSuggestion()
	want := filepath.Join(root, "alpha") + "/"
	if m.form.dir.Value() != want {
		t.Fatalf("dir = %q want %q", m.form.dir.Value(), want)
	}
	if m.form.dirAuto {
		t.Fatal("dirAuto should be cleared after completion")
	}
}

func TestGroupFormInheritsParentPath(t *testing.T) {
	m := buildModel(t)
	parentPath := t.TempDir()
	if err := m.store.CreateGroup("projects", parentPath); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.openGroupForm()
	if m.groupForm.path.Value() != parentPath {
		t.Fatalf("cursor on group should inherit its path, got %q want %q", m.groupForm.path.Value(), parentPath)
	}
	pickGroup(t, m, "")
	m.moveGroupCursor(0)
	if m.groupForm.path.Value() != "" {
		t.Fatalf("root parent should clear auto path, got %q", m.groupForm.path.Value())
	}

	m.groupForm.path.SetValue("/custom")
	m.groupForm.pathAuto = false
	pickGroup(t, m, "")
	m.moveGroupCursor(0)
	if m.groupForm.path.Value() != "/custom" {
		t.Fatalf("manual path should survive parent change, got %q", m.groupForm.path.Value())
	}
}

func TestAncestorGroupPathWalksUp(t *testing.T) {
	root := t.TempDir()
	m := &Model{groupPaths: map[string]string{"projects": root}}
	if got := m.ancestorGroupDir("projects/api/auth"); got != root {
		t.Fatalf("got %q want %q", got, root)
	}
	if got := m.ancestorGroupDir("other"); got != "" {
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
	m.groupForm.name.SetValue("relgrp")
	m.groupForm.path.SetValue("sub")
	if _, cmd := m.submitGroupForm(); cmd == nil {
		t.Fatalf("group form should submit, err=%q", m.err)
	}
	groups, _ := m.store.Groups()
	for _, g := range groups {
		if g.Name == "relgrp" && !filepath.IsAbs(g.Path) {
			t.Fatalf("group path stored relative: %q", g.Path)
		}
	}
}
