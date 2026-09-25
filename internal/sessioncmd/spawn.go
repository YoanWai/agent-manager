package sessioncmd

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/google/uuid"
)

// worktreeSetting is the store key holding the global spawn-in-worktree
// default the Agent Manager settings screen writes.
const worktreeSetting = "worktree_default"

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
}

func (s *Sessions) Create(sessionID string, opts CreateSessionOptions) (Session, error) {
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return Session{}, err
	}
	toolName := strings.TrimSpace(opts.Tool)
	if toolName == "" {
		// A spawn with no tool named runs whatever the caller runs, which a
		// terminal cannot supply: its tool is the user's shell. Guessing an
		// agent for it would start a CLI nobody asked for.
		if runtime.cfg.Tools[caller.Tool].Shell {
			return Session{}, fmt.Errorf("a terminal runs a shell, not an agent CLI, so there is none to inherit; name one with %s (configured tools are %s)", runtime.words.SpawnTool, strings.Join(agentToolNames(runtime), ", "))
		}
		toolName = caller.Tool
	}
	tool, known := runtime.cfg.Tools[toolName]
	if !known {
		return Session{}, fmt.Errorf("tool %q is not configured; configured tools are %s", toolName, strings.Join(agentToolNames(runtime), ", "))
	}
	if tool.Shell {
		return Session{}, fmt.Errorf("tool %q opens a shell, not an agent; use %s for that", toolName, runtime.words.CreateTerminal)
	}
	group, dir, err := runtime.createTarget(caller, opts.Group, opts.Directory)
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
	dir, worktree, err := s.prepareWorktree(dir, name, wantWorktree, opts.Worktree != nil)
	if err != nil {
		return Session{}, err
	}
	// Every failure from here on has to hand back the worktree it just
	// made: AddWorktree refuses a path that already exists, so a leaked
	// one turns the caller's retry into a name collision it cannot explain.
	discard := func() {
		if worktree.repo == "" {
			return
		}
		if driver, err := s.newGit(); err == nil {
			_, _ = driver.RemoveWorktreeIfClean(worktree.repo, dir, worktree.branch)
		}
	}

	plan := launch.Assemble(toolName, tool, prompt, autoNamed)
	manager := hooks.NewManager(s.configDir)
	command, env, err := launch.Environment(manager, toolName, tool, plan.Command, id)
	if err != nil {
		discard()
		return Session{}, err
	}
	sess := store.Session{
		ID:             id,
		Name:           name,
		Tool:           toolName,
		Cwd:            dir,
		Group:          group,
		Status:         status.Starting,
		AgentSessionID: plan.AgentSessionID,
		WorktreeRepo:   worktree.repo,
		WorktreeBranch: worktree.branch,
		PendingInputs:  plan.PendingInputs,
		LaunchPrompt:   plan.LaunchPrompt,
	}
	if err := runtime.createPane(sess.ID, sess.Cwd, command, env); err != nil {
		discard()
		return Session{}, err
	}
	sess.TmuxSocket = runtime.driver.SocketPath()
	if err := runtime.store.CreateSession(sess); err != nil {
		_ = runtime.driver.Kill(sess.ID)
		discard()
		return Session{}, err
	}
	_ = runtime.driver.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name))
	return runtime.sessionInfo(sess, true, false), nil
}

type worktreeTarget struct {
	repo   string
	branch string
}

// prepareWorktree opens the session's own checkout when one is wanted.
// A directory that cannot host one is only an error when the caller asked
// for a worktree by name; an inherited default degrades to a plain spawn,
// which is what the New Session form does rather than refusing to launch.
func (s *Sessions) prepareWorktree(dir, name string, wanted, explicit bool) (string, worktreeTarget, error) {
	if !wanted {
		return dir, worktreeTarget{}, nil
	}
	driver, err := s.newGit()
	if err != nil {
		if explicit {
			return "", worktreeTarget{}, fmt.Errorf("worktree sessions need git installed: %w", err)
		}
		return dir, worktreeTarget{}, nil
	}
	root, err := driver.RepoRoot(dir)
	if err != nil {
		if explicit {
			return "", worktreeTarget{}, fmt.Errorf("%s cannot host a worktree: %w; pass worktree false to spawn there anyway", dir, err)
		}
		return dir, worktreeTarget{}, nil
	}
	path, branch, err := driver.AddWorktree(root, name)
	if err != nil {
		return "", worktreeTarget{}, err
	}
	return path, worktreeTarget{repo: root, branch: branch}, nil
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

func agentToolNames(r *runtime) []string {
	names := make([]string, 0, len(r.cfg.Tools))
	for _, name := range r.cfg.ToolNames() {
		if !r.cfg.Tools[name].Shell {
			names = append(names, name)
		}
	}
	return names
}
