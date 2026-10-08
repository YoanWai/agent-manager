package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

var errRenameStageTwo = errors.New("second stage failed")

func TestRenameDefersGroupPersistence(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "kid", dir, "old/inner")

	for i, r := range railRows(m) {
		if r.isGroup && r.group == "old" {
			setRailCursor(m, i)
		}
	}
	m.openRename()
	if !m.rename.isGroup || m.rename.path != "old" {
		t.Fatalf("rename target wrong: %+v", m.rename)
	}
	m.rename.input.SetValue("fresh")
	m.rename.dir.SetValue(dir)
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("group rename dispatch returned no command")
	}
	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if g.Name == "fresh" || g.Name == "fresh/inner" {
			t.Fatalf("group rename persisted on the update path: %v", groups)
		}
	}
	if m.mode != modeRename {
		t.Fatalf("dialog should stay open until completion, mode=%v", m.mode)
	}
	m.applyCmd(t, cmd)
	kid := m.sessionRows()
	if len(kid) != 1 || kid[0].Group != "fresh/inner" {
		t.Fatalf("cascade failed: %+v", kid)
	}
	if m.mode != modeList {
		t.Fatalf("dialog should close on completion, mode=%v", m.mode)
	}
	if m.workspace.groupPaths["fresh"] != dir {
		t.Fatalf("default dir not mirrored: %v", m.workspace.groupPaths)
	}
}

func TestRenameDefersSessionPersistence(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "before", t.TempDir(), "")
	m.selectSessionRow(t, "before")
	m.openRename()
	m.rename.input.SetValue("after")
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("session rename dispatch returned no command")
	}
	if names := sessionNames(m); len(names) != 1 || names[0] != "before" {
		t.Fatalf("row renamed on the update path: %v", names)
	}
	m.applyCmd(t, cmd)
	if names := sessionNames(m); len(names) != 1 || names[0] != "after" {
		t.Fatalf("row not renamed on completion: %v", names)
	}
	if m.mode != modeList {
		t.Fatalf("dialog should close on completion, mode=%v", m.mode)
	}
}

func TestRenameShellChildCheckRunsInWorker(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	m.openRename()
	m.rename.input.SetValue("renamed")
	for index, name := range m.rename.toolNames {
		if name == "terminal" {
			m.rename.toolIndex = index
		}
	}
	_, cmd := m.applyRename()
	if cmd == nil {
		t.Fatal("rename did not return the queued worker command")
	}
	if m.errBar.text != "" {
		t.Fatalf("child store read ran on the update path: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	if m.errBar.text != "move its terminals first" {
		t.Fatalf("worker child precondition = %q", m.errBar.text)
	}
	stored, err := m.services.store.Get(m.rename.sessID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "coder" || m.isShell(stored.Tool) {
		t.Fatalf("failed precondition mutated the session: %+v", stored)
	}
}

func TestRenameGroupDirectoryCheckRunsBeforeMutation(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("old", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "old")
	m.openRename()
	m.rename.input.SetValue("new")
	m.rename.dir.SetValue("/definitely/missing/agent-manager")
	_, cmd := m.applyRename()
	groups, _ := m.services.store.Groups()
	if len(groups) != 1 || groups[0].Name != "old" {
		t.Fatalf("rename mutated before directory preflight: %+v", groups)
	}
	m.applyCmd(t, cmd)
	groups, _ = m.services.store.Groups()
	if len(groups) != 1 || groups[0].Name != "old" {
		t.Fatalf("failed directory preflight mutated group: %+v", groups)
	}
	if !strings.Contains(m.errBar.text, "default path does not exist") {
		t.Fatalf("directory preflight error = %q", m.errBar.text)
	}
}

func TestRenameKeepsCapturedRowIdentity(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "captured", t.TempDir(), "")
	m.selectSessionRow(t, "captured")
	m.openRename()
	m.rename.input.SetValue("saved")
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	for i := range m.workspace.sessions {
		m.workspace.sessions[i].Name = "mutated"
	}
	m.applyCmd(t, cmd)
	if names := sessionNames(m); len(names) != 1 || names[0] != "saved" {
		t.Fatalf("completion used the live row, want the captured one: %v", names)
	}
}

func TestRenameGroupPartialFailureReconcilesCommittedStages(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "kid", dir, "old/inner")

	request := renameRequest{
		kind:     renameGroup,
		oldGroup: "old",
		newGroup: "fresh",
		dir:      dir,
		name:     "fresh",
		gen:      7,
	}
	job := &effectJob{request: request}
	m.mode = modeRename
	m.gens.dialog = 7
	m.rename.renameTarget = renameTarget{isGroup: true, path: "old"}
	m.applyRenameEffect(job, renameEffectResult{groupRenamed: true}, errRenameStageTwo)

	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if g.Name == "old" {
			t.Fatalf("committed rename lost: %v", groups)
		}
	}
	kid := m.sessionRows()
	if len(kid) != 1 || kid[0].Group != "fresh/inner" {
		t.Fatalf("committed rename not mirrored locally: %+v", kid)
	}
	if m.workspace.groupPaths["fresh"] != "" {
		t.Fatalf("uncommitted default dir mirrored: %v", m.workspace.groupPaths)
	}
	if m.mode != modeRename {
		t.Fatalf("dialog should stay open on failure, mode=%v", m.mode)
	}
	if m.errBar.text != "second stage failed" {
		t.Fatalf("errBar=%q", m.errBar.text)
	}
	var renderedFresh bool
	for _, r := range railRows(m) {
		if r.isGroup && r.group == "fresh" {
			renderedFresh = true
		}
	}
	if !renderedFresh {
		t.Fatal("committed rename not rendered before the error")
	}
}

func TestRenameCompletionDoesNotReplaceNewerDialog(t *testing.T) {
	m := buildModel(t)
	for _, name := range []string{"one", "two"} {
		if err := m.services.store.CreateGroup(name, ""); err != nil {
			t.Fatalf("create group: %v", err)
		}
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectGroupRow(t, "one")
	m.openRename()
	m.rename.input.SetValue("one-renamed")
	_, cmd1 := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd1 == nil {
		t.Fatal("first rename dispatch returned no command")
	}
	m.selectGroupRow(t, "two")
	m.openRename()
	m.rename.input.SetValue("two-renamed")
	_, _ = m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})

	updated, _ := m.Update(cmd1())
	*m = *updated.(*Model)
	if m.mode != modeRename || m.rename.path != "two" {
		t.Fatalf("older completion replaced the newer dialog: mode=%v path=%q", m.mode, m.rename.path)
	}
	m.drainEffects(t)
	groups, _ := m.services.store.Groups()
	renamed := map[string]bool{}
	for _, g := range groups {
		renamed[g.Name] = true
	}
	if !renamed["one-renamed"] || !renamed["two-renamed"] {
		t.Fatalf("expected both renames committed: %v", groups)
	}
	if m.mode != modeList {
		t.Fatalf("dialog should close on the matching completion, mode=%v", m.mode)
	}
}

func TestRenameCommandExecutesOnceAndCompletionIsIdempotent(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "once", t.TempDir(), "")
	m.selectSessionRow(t, "once")
	m.openRename()
	m.rename.input.SetValue("twice")
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	first := cmd()
	second := cmd()
	firstMsg, ok := first.(effectCompletedMsg)
	if !ok {
		t.Fatalf("first run returned %+v", first)
	}
	secondMsg, ok := second.(effectCompletedMsg)
	if !ok {
		t.Fatalf("second run returned %+v", second)
	}
	if firstMsg.id != secondMsg.id {
		t.Fatal("second run produced a different completion")
	}
	updated, _ := m.Update(first)
	*m = *updated.(*Model)
	updated, _ = m.Update(second)
	*m = *updated.(*Model)
	if names := sessionNames(m); len(names) != 1 || names[0] != "twice" {
		t.Fatalf("completion applied twice: %v", names)
	}
	stored, _ := m.services.store.ListSessions(false)
	if len(stored) != 1 || stored[0].Name != "twice" {
		t.Fatalf("store renamed twice: %+v", stored)
	}
}

func TestQuitDrainsRenameEffects(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "kid", dir, "old/inner")
	for i, r := range railRows(m) {
		if r.isGroup && r.group == "old" {
			setRailCursor(m, i)
		}
	}
	m.openRename()
	m.rename.input.SetValue("fresh")
	m.rename.dir.SetValue(dir)
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("group rename dispatch returned no command")
	}
	_, _ = m.requestQuit()
	if !m.effects.quitting {
		t.Fatal("quit not queued")
	}
	m.applyCmd(t, cmd)
	m.drainEffects(t)
	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if g.Name == "old" {
			t.Fatalf("quit drained the queue without committing: %v", groups)
		}
	}
}

func TestMoveSessionDefersPlacement(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("target", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "")

	m.selectSessionRow(t, "wanderer")
	m.openMove()
	pickGroup(t, m, "target")
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("move dispatch returned no command")
	}
	got, err := m.services.store.Get(m.move.id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Group != "" {
		t.Fatalf("session placed on the update path: %+v", got)
	}
	if m.mode != modeMove {
		t.Fatalf("dialog should stay open until completion, mode=%v", m.mode)
	}
	m.applyCmd(t, cmd)
	got, _ = m.services.store.Get(m.move.id)
	if got.Group != "target" {
		t.Fatalf("move not committed on completion: %+v", got)
	}
	if m.mode != modeList {
		t.Fatalf("dialog should close on completion, mode=%v", m.mode)
	}
}

func TestMoveWorkerRejectsChangedSourcePrecondition(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	for _, group := range []string{"target", "concurrent"} {
		if err := m.services.store.CreateGroup(group, dir); err != nil {
			t.Fatal(err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "")
	m.selectSessionRow(t, "wanderer")
	m.openMove()
	pickGroup(t, m, "target")
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	if err := m.services.store.PlaceSession(m.move.id, "concurrent", ""); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, cmd)
	stored, err := m.services.store.Get(m.move.id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Group != "concurrent" {
		t.Fatalf("stale move overwrote concurrent placement: %+v", stored)
	}
	if m.errBar.text != "move source changed before placement" {
		t.Fatalf("precondition error = %q", m.errBar.text)
	}
}

func TestMoveGroupDefersPersistence(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := m.services.store.CreateGroup("beta", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "alpha/inner")

	m.selectGroupRow(t, "alpha/inner")
	m.openMove()
	pickGroup(t, m, "beta")
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("move dispatch returned no command")
	}
	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if g.Name == "beta/inner" {
			t.Fatalf("group moved on the update path: %v", groups)
		}
	}
	m.applyCmd(t, cmd)
	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "beta/inner" {
		t.Fatalf("group move failed: %+v", sessions)
	}
	if m.mode != modeList {
		t.Fatalf("dialog should close on completion, mode=%v", m.mode)
	}
}

func TestMoveCompletionDoesNotReplaceNewerDialog(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	for _, name := range []string{"target", "other"} {
		if err := m.services.store.CreateGroup(name, ""); err != nil {
			t.Fatalf("create group: %v", err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "")

	m.selectSessionRow(t, "wanderer")
	m.openMove()
	pickGroup(t, m, "target")
	_, cmd1 := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd1 == nil {
		t.Fatal("first move dispatch returned no command")
	}
	m.selectSessionRow(t, "wanderer")
	m.openMove()
	pickGroup(t, m, "other")
	_, _ = m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})

	// The lane runs four jobs: place target, close target dialog (must not
	// match), place other, close other dialog.
	updated, _ := m.Update(cmd1())
	*m = *updated.(*Model)
	if m.mode != modeMove {
		t.Fatalf("older completion closed the newer dialog, mode=%v", m.mode)
	}
	if m.form.groups[m.form.groupIndex].path != "other" {
		t.Fatalf("newer dialog selection changed: %+v", m.form.groups)
	}
	m.drainEffects(t)
	got, err := m.services.store.Get(m.move.id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Group != "other" {
		t.Fatalf("second move not committed: %+v", got)
	}
	if m.mode != modeList {
		t.Fatalf("dialog should close on the matching completion, mode=%v", m.mode)
	}
}

// runRename drives the worker directly: the durable stages must commit in
// order, and the result must say exactly which stages landed.
func TestRenameCompletionDoesNotCloseResubmittedDialog(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "before", t.TempDir(), "")
	m.selectSessionRow(t, "before")
	m.openRename()
	m.rename.input.SetValue("after")
	_, cmd1 := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd1 == nil {
		t.Fatal("first dispatch returned no command")
	}
	// Same dialog, same name; resubmit without reopening (e.g. after
	// cycling the tool). gens.dialog advanced, so cmd1's completion is stale.
	m.rename.cycleTool(1)
	_, _ = m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.effects.pending) == 0 {
		t.Fatal("resubmit did not enqueue a job")
	}

	updated, _ := m.Update(cmd1())
	*m = *updated.(*Model)
	if m.mode != modeRename {
		t.Fatalf("older completion closed the resubmitted dialog, mode=%v", m.mode)
	}
	m.drainEffects(t)
	if m.mode != modeList {
		t.Fatalf("matching completion did not close the dialog, mode=%v", m.mode)
	}
	if names := sessionNames(m); len(names) != 1 || names[0] != "after" {
		t.Fatalf("rename not applied: %v", names)
	}
}

func TestRenameCompletionDoesNotCloseReopenedSameTargetDialog(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("one", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectGroupRow(t, "one")
	m.openRename()
	m.rename.input.SetValue("renamed")
	_, cmd1 := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd1 == nil {
		t.Fatal("first dispatch returned no command")
	}
	// Reopen the same group; the old completion must not close this fresh
	// dialog. The reopened rename targets a new name because the first one
	// already committed "renamed".
	m.mode = modeList
	m.selectGroupRow(t, "one")
	m.openRename()
	if m.rename.path != "one" {
		t.Fatalf("reopened wrong target: %q", m.rename.path)
	}
	m.rename.input.SetValue("renamed-again")

	updated, _ := m.Update(cmd1())
	*m = *updated.(*Model)
	if m.mode != modeRename {
		t.Fatalf("older completion closed the reopened dialog, mode=%v", m.mode)
	}
	if m.rename.input.Value() != "renamed-again" {
		t.Fatalf("older completion clobbered the reopened input: %q", m.rename.input.Value())
	}
	_, _ = m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)
	if m.mode != modeList {
		t.Fatalf("matching completion did not close the dialog, mode=%v", m.mode)
	}
}

func TestRenameCompletionFencesStalePolls(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "kid", dir, "old/inner")

	for i, r := range railRows(m) {
		if r.isGroup && r.group == "old" {
			setRailCursor(m, i)
		}
	}
	m.openRename()
	m.rename.input.SetValue("fresh")
	m.rename.dir.SetValue(dir)
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("group rename dispatch returned no command")
	}

	// A poll pass that read the list before the rename committed; its
	// listedAt predates the completion, so it must be fenced out.
	listedAt := time.Now().Add(-time.Second)
	staleSessions := []store.Session{{ID: "kid", Name: "kid", Group: "old/inner", Tool: "claude", Cwd: dir}}
	staleGroups := []string{"old", "old/inner"}
	stalePaths := map[string]string{"old": dir}

	m.applyCmd(t, cmd)
	if m.effects.latestObservation.IsZero() {
		t.Fatal("rename completion did not advance the observation fence")
	}

	m.Update(refreshMsg{sessions: staleSessions, listedAt: listedAt, groups: staleGroups, groupPaths: stalePaths})
	for _, g := range m.workspace.groups {
		if strings.HasPrefix(g, "old") {
			t.Fatalf("stale poll restored the pre-rename group: %v", m.workspace.groups)
		}
	}
	if !hasWorkspaceGroup(m, "fresh/inner") {
		t.Fatalf("committed rename missing from workspace after stale poll: %v", m.workspace.groups)
	}
}

func hasWorkspaceGroup(m *Model, path string) bool {
	for _, g := range m.workspace.groups {
		if g == path {
			return true
		}
	}
	return false
}

func TestRenameWorkerCommitsStagesInOrder(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "kid", dir, "old/inner")

	services := effectServices{store: m.services.store, driver: m.services.tmux, gitDrv: m.services.gitDrv}
	result, err := services.runRename(renameRequest{
		kind:     renameGroup,
		oldGroup: "old",
		newGroup: "fresh",
		dir:      dir,
		worktree: "on",
		name:     "fresh",
	})
	if err != nil {
		t.Fatalf("worker: %v", err)
	}
	r := result.(renameEffectResult)
	if !r.groupRenamed || !r.pathSet || !r.worktreeSet {
		t.Fatalf("stages not all committed: %+v", r)
	}
	if len(r.sessions) != 1 || r.sessions[0].Group != "fresh/inner" {
		t.Fatalf("relabel set wrong: %+v", r.sessions)
	}
	groups, _ := m.services.store.Groups()
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g.Name] = true
	}
	if !seen["fresh"] || !seen["fresh/inner"] {
		t.Fatalf("store not renamed: %v", groups)
	}
}
