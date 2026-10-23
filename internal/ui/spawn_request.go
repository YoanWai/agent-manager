package ui

import (
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
)

// formSpawnRequest names the session, places it in the picked group and
// captures the group's launch inputs and the prompt's images.
func (m *Model) formSpawnRequest(request spawn.Request) spawnRequest {
	name, autoNamed := request.Name, request.Name == ""
	if autoNamed {
		name = request.Tool + "-" + newID()[:4]
	}
	group := m.form.groupPath()
	defaults := m.spawnDefaults(group)
	return spawnRequest{
		kind:         spawnForm,
		toolName:     request.Tool,
		name:         name,
		group:        group,
		prompt:       launch.WithInstructions(request.Saved, request.Prompt),
		autoNamed:    autoNamed,
		pickWorktree: request.Worktree,
		choice:       config.Choice(request.Pick),
		base:         defaults.base,
		pane:         defaults.pane,
		composerGen:  m.form.prompt.gen,
		images:       m.form.prompt.attachments,
		draft:        m.form.prompt.input.Value(),
		draftName:    m.form.Name(),
		draftDir:     m.form.Dir(),
		rawDir:       request.Dir,
		dirFallbacks: defaults.fallbacks,
		wantWorktree: request.Worktree,
	}
}

// formSpawnDir is the directory the form would launch in, resolved the
// same way submit resolves it.
func (m *Model) formSpawnDir() string {
	return m.capturedAbsolutePath(m.form.Dir(), m.env.workDir)
}

// capturedAbsolutePath normalizes a displayed path without consulting the
// filesystem. It uses the startup directory/home captured before the event
// loop began.
func (m *Model) capturedAbsolutePath(raw, fallback string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		path = fallback
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		path = filepath.Join(m.env.homeDir, strings.TrimPrefix(path, "~"))
	}
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(m.env.workDir, path)
	}
	return filepath.Clean(path)
}

// spawnDefaults are the root's launch inputs for a spawn into a group,
// read when the form or the quick bar builds its request.
type spawnDefaults struct {
	// base is the ref a worktree branches from, or "" to detect it.
	base string
	pane sessioncmd.PaneSize
	// groupDir is the group's own default path, and fallbacks the nearest
	// ancestor's and then the startup directory.
	groupDir  string
	fallbacks []string
	worktree  bool
}

func (m *Model) spawnDefaults(group string) spawnDefaults {
	paneW, paneH := m.paneTargetSize()
	return spawnDefaults{
		base:      m.groupBase(group),
		pane:      sessioncmd.PaneSize{Width: paneW, Height: paneH},
		groupDir:  m.workspace.groupPaths[group],
		fallbacks: m.groupDirCandidates(group),
		worktree:  m.cachedSpawnWorktreeDefault(group),
	}
}

// groupDirCandidates captures the same nearest-ancestor preference as the
// old synchronous lookup. Validation happens later inside the command.
func (m *Model) groupDirCandidates(group string) []string {
	candidates := make([]string, 0, 4)
	for current := group; current != ""; current = parentGroup(current) {
		if path := m.workspace.groupPaths[current]; path != "" {
			candidates = append(candidates, path)
		}
	}
	if m.env.workDir != "" {
		candidates = append(candidates, m.env.workDir)
	}
	return candidates
}

func (m *Model) capturedGroupDefaultDir(group string) string {
	candidates := m.groupDirCandidates(group)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func (m *Model) capturedAncestorGroupDir(group string) string {
	for current := group; current != ""; current = parentGroup(current) {
		if path := m.workspace.groupPaths[current]; path != "" {
			return path
		}
	}
	return ""
}
