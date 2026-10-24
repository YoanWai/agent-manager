package ui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFormWorktreeToggleSeedsFromSetting(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	if m.form.Worktree() {
		t.Fatal("worktree should default off with no setting")
	}
	m.mode = modeList
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyTestMsg(t, m.openForm()())
	if !m.form.Worktree() {
		t.Fatal("worktree should seed on from setting")
	}
}

func TestFormWorktreeGatedInNonRepoDir(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	plain := t.TempDir()
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.openForm()
	m.form.SetDir(plain)
	if m.formWorktreeOn() {
		t.Fatal("a non-repo dir cannot host a worktree, even with the setting on")
	}
	if view := m.viewForm(); !strings.Contains(view, worktreeUnavailable) {
		t.Fatalf("form should mark worktree unavailable, got %q", view)
	}
	m.form.FocusField(m.spawnHost(), spawn.FieldWorktree)
	_, probe := m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if probe != nil {
		m.applyTestMsg(t, probe())
	}
	if m.formWorktreeOn() {
		t.Fatal("toggling must not turn worktree on for a non-repo dir")
	}
	if !strings.Contains(m.errBar.text, "need a git repository") {
		t.Fatalf("refused toggle should say why, got %q", m.errBar.text)
	}
	_, spawn := m.submitForm()
	m.applyCmd(t, spawn)
	sessions, err := m.services.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("gated form should still launch a plain session, got %d", len(sessions))
	}
	if sessions[0].WorktreeRepo != "" {
		t.Fatalf("gated spawn must not record a worktree: %q", sessions[0].WorktreeRepo)
	}
}

func TestWorktreeCapabilityExpiresSoAFreshRepoIsSeen(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	root := t.TempDir()
	dir, seed := filepath.Join(root, "repo"), filepath.Join(root, "seed")
	for _, path := range []string{dir, seed} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// git init can outlast worktreeLookupTTL on Windows, so the repo is built
	// before the answer is cached and only moved into place after.
	initGitRepo(t, seed)
	m.openForm()
	m.form.SetDir(dir)
	m.applyTestMsg(t, m.formWorktreeProbeCmd(false)())
	if capable, known := m.cachedWorktreeCapability(dir); !known || capable {
		t.Fatalf("plain directory cache = capable %t known %t", capable, known)
	}
	if err := os.Rename(filepath.Join(seed, ".git"), filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	if capable, known := m.cachedWorktreeCapability(dir); !known || capable {
		t.Fatalf("fresh cache changed without a probe: capable %t known %t", capable, known)
	}
	answer := m.ledger.worktreeRepos[dir]
	answer.at = answer.at.Add(-worktreeLookupTTL)
	m.ledger.worktreeRepos[dir] = answer
	if _, known := m.cachedWorktreeCapability(dir); known {
		t.Fatal("expired capability still appeared current")
	}
	m.applyTestMsg(t, m.formWorktreeProbeCmd(false)())
	if capable, known := m.cachedWorktreeCapability(dir); !known || !capable {
		t.Fatalf("fresh probe cache = capable %t known %t", capable, known)
	}
}

type blockedRepoPreflight struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	capable bool
}

func (r *blockedRepoPreflight) resolve(string, []string) (string, bool) { return "", false }

func (r *blockedRepoPreflight) repoCapable(string) bool {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.capable
}

func TestWorktreeProbeDefersGitAndRejectsReopenedForm(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	dir := t.TempDir()
	m.form.SetDir(dir)
	reader := &blockedRepoPreflight{
		started: make(chan struct{}), release: make(chan struct{}), capable: true,
	}
	m.gens.worktreeProbe++
	request := worktreeProbeRequest{
		target: worktreeProbeForm, generation: m.form.prompt.gen,
		sequence: m.gens.worktreeProbe, dir: m.formSpawnDir(), toggle: true, from: m.form.Worktree(),
	}
	cmd := worktreeProbeCmd(request, reader)
	select {
	case <-reader.started:
		t.Fatal("repo probe ran on the update path")
	default:
	}
	completed := make(chan worktreeProbeMsg, 1)
	go func() { completed <- cmd().(worktreeProbeMsg) }()
	<-reader.started
	m.openForm()
	close(reader.release)
	m.handleWorktreeProbe(<-completed)
	if m.form.Worktree() {
		t.Fatal("old repo probe toggled the reopened form")
	}
	if _, known := m.cachedWorktreeCapability(dir); known {
		t.Fatal("stale repo probe populated rendering facts")
	}
}

func TestPrepareFrameConsumesCapturedWorktreeFacts(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	m.ledger.worktreeRepos = nil
	m.prepareFrame()
	if m.ledger.worktreeRepos != nil {
		t.Fatal("rendering performed a repository lookup")
	}
}

func TestFormWorktreeStaysOnInRepoDir(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyTestMsg(t, m.openForm()())
	m.form.SetDir(repo)
	m.applyTestMsg(t, m.formWorktreeProbeCmd(false)())
	if !m.formWorktreeOn() {
		t.Fatal("a repo dir should keep the worktree default on")
	}
	m.form.FocusField(m.spawnHost(), spawn.FieldWorktree)
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.formWorktreeOn() {
		t.Fatal("toggle should turn worktree off in a repo dir")
	}
}
