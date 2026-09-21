package ui

import (
	"context"
	"time"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) remoteDiffCmd(sess store.Session, scope git.Scope, gen int, repoWant, base string, refresh bool) tea.Cmd {
	ref, _ := m.remoteRef(sess.ID)
	client := m.federation.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		review, err := client.Review(ctx, ref.Host, sess.Cwd, scope, base, repoWant, "")
		return diffLoadedMsg{sessID: sess.ID, scope: scope, gen: gen, refresh: refresh, err: err,
			set: review.Set, repoRoots: review.Roots, repoRoot: review.Set.Repo.Root,
			fp: review.Fingerprint, worktrees: review.Worktrees}
	}
}
