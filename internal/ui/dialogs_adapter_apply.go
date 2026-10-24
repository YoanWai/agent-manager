package ui

import (
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
)

// dialogSources is what the fork and rename workers reach off the update
// path, captured when their request was queued.
func (s effectServices) dialogSources() uiapply.Sources {
	return uiapply.Sources{
		Store:     s.store,
		Git:       s.gitDrv,
		Config:    s.cfg,
		Panes:     s.driver,
		Lifecycle: s.lifecycle,
		Runner:    s.runner,
	}
}

// dialogPathProbe is the read-only path probe the dialog workers read with:
// the spawn effect's, which resolves a default path the way a group form
// does, plus the plain directory test a fork's source needs.
type dialogPathProbe struct{ systemDirectoryPreflight }

func (p dialogPathProbe) Resolve(raw string, fallbacks []string) (string, bool) {
	return p.resolve(raw, fallbacks)
}

func (p dialogPathProbe) ResolveDir(path string) bool { return uilaunch.IsDir(path) }

func (s effectServices) dialogPathProbe() uiapply.Preflight {
	return dialogPathProbe{systemDirectoryPreflight{git: s.gitDrv}}
}
