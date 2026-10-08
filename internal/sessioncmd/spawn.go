package sessioncmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/google/uuid"
)

// worktreeSetting is the store key holding the global spawn-in-worktree
// default the Agent Manager settings screen writes.
const worktreeSetting = "worktree_default"

// baseFetchSetting is the store key that, set to "off", skips the fetch
// ahead of a worktree spawn.
const baseFetchSetting = "worktree_fetch"

type CreateSessionOptions struct {
	Tool string
	Name string
	// Nil inherits the calling session's group; a pointer to an empty string
	// deliberately targets the root group.
	Group     *string
	Directory string
	Prompt    string
	// Nil inherits the group's spawn-in-worktree choice, then the global
	// setting.
	Worktree *bool
	// Model, Effort and Profile ride every later launch of the session.
	Model   string
	Effort  string
	Profile string
}

func (s *Sessions) Create(sessionID string, opts CreateSessionOptions) (Session, error) {
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.Close()
	caller, err := runtime.optionalCaller(sessionID)
	if err != nil {
		return Session{}, err
	}
	toolName := strings.TrimSpace(opts.Tool)
	if toolName == "" {
		if toolName, err = runtime.toolFor(caller); err != nil {
			return Session{}, err
		}
	}
	tool, known := runtime.cfg.Tools[toolName]
	if !known {
		return Session{}, fmt.Errorf("tool %q is not configured; configured tools are %s", toolName, strings.Join(runtime.cfg.AgentToolNames(), ", "))
	}
	if tool.Shell {
		return Session{}, fmt.Errorf("tool %q opens a shell, not an agent; use %s for that", toolName, runtime.words.CreateTerminal)
	}
	group, dir, err := runtime.createTarget(caller, opts.Group, opts.Directory)
	if err != nil {
		return Session{}, err
	}
	choice, err := s.choose(runtime.words, toolName, tool, opts)
	if err != nil {
		return Session{}, err
	}
	prompt := strings.TrimSpace(opts.Prompt)
	if strings.HasPrefix(prompt, "-") && tool.PromptFlag == "" {
		return Session{}, fmt.Errorf(`prompt cannot start with "-" for %s, which takes its prompt as a bare argument and would read it as a flag`, toolName)
	}
	name := strings.TrimSpace(opts.Name)
	autoNamed := name == ""
	id := uuid.NewString()[:8]
	if autoNamed {
		name = toolName + "-" + id[:4]
	}

	wantWorktree, err := runtime.worktreeWanted(group, opts.Worktree)
	if err != nil {
		return Session{}, err
	}
	base, err := runtime.groupBase(group)
	if err != nil {
		return Session{}, err
	}
	fetchSetting, err := runtime.store.Setting(baseFetchSetting)
	if err != nil {
		return Session{}, err
	}
	proactive, err := runtime.store.ProactiveCoordination()
	if err != nil {
		return Session{}, err
	}
	worktreeGit, err := s.worktreeGit(dir, wantWorktree, opts.Worktree != nil)
	if err != nil {
		return Session{}, err
	}
	lifecycle, err := s.lifecycle(runtime, worktreeGit)
	if err != nil {
		return Session{}, err
	}
	var worktree *WorktreeRequest
	if worktreeGit != nil {
		worktree = &WorktreeRequest{Base: base, Fetch: fetchSetting != "off"}
	}
	launched, err := lifecycle.Spawn(SpawnRequest{
		Session:  store.Session{ID: id, Name: name, Tool: toolName, Cwd: dir, Group: group, Choice: choice},
		Tool:     tool,
		Plan:     launch.Assemble(toolName, tool.WithChoice(choice), prompt, autoNamed, proactive),
		Worktree: worktree,
	})
	if err != nil {
		return Session{}, err
	}
	return runtime.sessionInfo(launched.Session, true, false), nil
}

// toolFor is the CLI a spawn runs when it names none. A terminal or a script
// runs no agent to copy, so it takes the one picked in Settings.
func (r *runtime) toolFor(caller store.Session) (string, error) {
	if caller.ID == "" || r.cfg.Tools[caller.Tool].Shell {
		return r.settingsTool()
	}
	return caller.Tool, nil
}

func (r *runtime) settingsTool() (string, error) {
	chosen, err := r.store.DefaultTool()
	if err != nil {
		return "", err
	}
	hidden, err := r.store.HiddenTools()
	if err != nil {
		return "", err
	}
	if name := r.cfg.DefaultAgentTool(chosen, hidden); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("every agent CLI is turned off for new sessions in settings; name one with %s (configured tools are %s)", r.words.SpawnTool, strings.Join(r.cfg.AgentToolNames(), ", "))
}

// worktreeGit is the driver that opens the session's own checkout, or nil
// for a plain spawn. A directory that cannot host one is only an error when
// the caller asked for a worktree by name; an inherited default degrades to
// a plain spawn, which is what the New Session form does rather than
// refusing to launch.
func (s *Sessions) worktreeGit(dir string, wanted, explicit bool) (*git.Driver, error) {
	if !wanted {
		return nil, nil
	}
	driver, err := s.newGit()
	if err != nil {
		if explicit {
			return nil, fmt.Errorf("worktree sessions need git installed: %w", err)
		}
		return nil, nil
	}
	if _, err := driver.RepoRoot(dir); err != nil {
		if explicit {
			return nil, fmt.Errorf("%s cannot host a worktree: %w; pass worktree false to spawn there anyway", dir, err)
		}
		return nil, nil
	}
	return driver, nil
}

// worktreeWanted resolves whether a spawn opens its own worktree: an
// explicit choice wins, then the nearest ancestor group with one, then
// the global setting.
func (r *runtime) worktreeWanted(group string, explicit *bool) (bool, error) {
	if explicit != nil {
		return *explicit, nil
	}
	groups, err := r.store.Groups()
	if err != nil {
		return false, err
	}
	choice := make(map[string]string, len(groups))
	for _, candidate := range groups {
		choice[candidate.Name] = candidate.Worktree
	}
	for current := group; current != ""; current = parentGroup(current) {
		switch choice[current] {
		case "on":
			return true, nil
		case "off":
			return false, nil
		}
	}
	setting, err := r.store.Setting(worktreeSetting)
	if err != nil {
		return false, err
	}
	return setting == "on", nil
}

// groupBase is the ref a spawn into group branches from: the nearest
// ancestor group's choice, or "" to detect the repo's default branch.
func (r *runtime) groupBase(group string) (string, error) {
	return groupBase(r.store, group)
}

func groupBase(st *store.Store, group string) (string, error) {
	groups, err := st.Groups()
	if err != nil {
		return "", err
	}
	bases := make(map[string]string, len(groups))
	for _, candidate := range groups {
		bases[candidate.Name] = candidate.Base
	}
	for current := group; current != ""; current = parentGroup(current) {
		if base := bases[current]; base != "" {
			return base, nil
		}
	}
	return "", nil
}

// createTarget resolves the group and directory a new pane opens in.
// A nil requested group inherits the caller's; an explicit one must
// already exist. An explicit directory wins outright; a caller that named
// a group falls back to that group's nearest inherited default path; a
// caller that named none opens beside itself.
func (r *runtime) createTarget(caller store.Session, requestedGroup *string, directory string) (string, string, error) {
	group := caller.Group
	groups, err := r.store.Groups()
	if err != nil {
		return "", "", err
	}
	byName := make(map[string]store.Group, len(groups))
	archived := make(map[string]bool, len(groups))
	for _, candidate := range groups {
		byName[candidate.Name] = candidate
		archived[candidate.Name] = candidate.Archived
	}
	if requestedGroup != nil {
		group = strings.TrimSpace(*requestedGroup)
		if group != "" {
			if _, ok := byName[group]; !ok {
				return "", "", fmt.Errorf("group %q does not exist; call %s for the existing ones or %s to add it", group, r.words.ListGroups, r.words.CreateGroup)
			}
		}
	}
	if group != "" && store.EffectivelyArchived(archived, group) {
		return "", "", fmt.Errorf("group %q is archived; restore it in Agent Manager first", group)
	}
	if strings.TrimSpace(directory) != "" {
		dir, err := resolveTerminalDirectory(directory)
		return group, dir, err
	}
	if requestedGroup != nil {
		for current := group; current != ""; current = parentGroup(current) {
			if candidate := byName[current].Path; candidate != "" {
				if dir, err := resolveTerminalDirectory(candidate); err == nil {
					return group, dir, nil
				}
			}
		}
	}
	dir := caller.Cwd
	if caller.ID == "" {
		// There is no pane to ask. tmux would read the empty id's target am_
		// as a prefix and answer with another session's directory.
		if dir, err = os.Getwd(); err != nil {
			return "", "", err
		}
	} else if current, err := r.driver.PaneCurrentPath(caller.ID); err == nil {
		dir = current
	}
	resolved, err := resolveTerminalDirectory(dir)
	if err != nil {
		return "", "", fmt.Errorf("no usable directory for terminal: %w", err)
	}
	return group, resolved, nil
}

func resolveTerminalDirectory(raw string) (string, error) {
	dir := strings.TrimSpace(raw)
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if dir == "~" {
			dir = home
		} else {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("directory %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return abs, nil
}

// SessionLabel renders a session's identity for the tmux status bar.
func SessionLabel(group, name string) string {
	if group == "" {
		return name
	}
	return group + " · " + name
}
