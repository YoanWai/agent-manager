package launch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
)

// Kind says which surface asked for the spawn: the form, the quick bar, or
// the terminal key, which opens a shell with no prompt of its own.
type Kind uint8

const (
	Form Kind = iota
	Quick
	Shell
	Import
)

// Request captures everything a spawn needs at dispatch time, so the worker
// never reads the model: the tool, the captured launch inputs, the pane size
// the manager draws, and the images the prompt names, moved out of the
// composer that held them.
type Request struct {
	ID           string
	Kind         Kind
	ToolName     string
	Name         string
	Dir          string
	Group        string
	ParentID     string
	Prompt       string
	AutoNamed    bool
	Worktree     bool
	PickWorktree bool
	Pane         sessioncmd.PaneSize
	ComposerGen  int
	DraftName    string
	DraftDir     string
	Draft        string
	RawDir       string
	DirFallbacks []string
	WantWorktree bool
	DirReader    DirectoryPreflight
	TerminalDir  TerminalDirectoryRequest
	TerminalRead TerminalDirectoryReader
	Plan         *launch.Plan
	Images       []composer.Attachment
	Choice       config.Choice
	Base         string
	Environment  string
}

// Result is the durable outcome of a spawn: the persisted session row, which
// exists only when the pane was created too, and a label error that stays a
// warning after committed placement.
type Result struct {
	Plan        *launch.Plan
	Session     store.Session
	Label       error
	Dir         string
	Worktree    bool
	RepoKnown   bool
	RepoCapable bool
}

// Services is what a spawn asks of the manager: the lifecycle service that
// creates the row and the pane, and the store that keeps the group and the
// coordination setting.
type Services struct {
	Lifecycle *sessioncmd.Lifecycle
	Store     *store.Store
	Config    config.Config
	Git       *git.Driver
}

func Run(services Services, request Request) (Result, error) {
	result := Result{}
	if services.Lifecycle == nil {
		return result, errors.New("spawn requires the lifecycle service")
	}
	tool := services.Config.Tools[request.ToolName].WithChoice(request.Choice)
	dir := request.Dir
	if dir == "" {
		if request.Kind == Shell {
			reader := request.TerminalRead
			if reader == nil {
				reader = SystemTerminalDirectoryReader{Dirs: SystemDirectoryPreflight{Git: services.Git}}
			}
			var ok bool
			dir, ok = reader.Resolve(request.TerminalDir)
			if !ok {
				return result, errors.New("no directory to open a terminal in")
			}
		} else {
			reader := request.DirReader
			if reader == nil {
				reader = SystemDirectoryPreflight{Git: services.Git}
			}
			var ok bool
			dir, ok = reader.Resolve(request.RawDir, request.DirFallbacks)
			if !ok {
				prefix := "working directory does not exist: "
				if request.Kind == Quick {
					prefix = "group has no valid default path: "
				}
				return result, errors.New(prefix + dir)
			}
			if request.WantWorktree {
				result.RepoKnown = true
				result.RepoCapable = reader.RepoCapable(dir)
				request.Worktree = result.RepoCapable
			}
		}
	}
	result.Dir = dir
	result.Worktree = request.Worktree
	plan := launch.Plan{}
	if request.Kind == Shell {
		plan.Command = tool.Command
	} else if request.Plan != nil {
		plan = *request.Plan
	} else {
		proactive, err := services.Store.ProactiveCoordination()
		if err != nil {
			return result, err
		}
		plan = launch.Assemble(request.ToolName, tool, request.Prompt, request.AutoNamed, proactive)
	}
	result.Plan = &plan
	var worktree *sessioncmd.WorktreeRequest
	if request.Worktree {
		worktree = &sessioncmd.WorktreeRequest{Base: request.Base}
	}
	launched, err := services.Lifecycle.Spawn(sessioncmd.SpawnRequest{
		Session: store.Session{
			ID:          request.ID,
			Name:        request.Name,
			Tool:        request.ToolName,
			Cwd:         dir,
			Group:       request.Group,
			ParentID:    request.ParentID,
			Choice:      request.Choice,
			Environment: request.Environment,
		},
		Tool:     tool,
		Plan:     plan,
		Pane:     request.Pane,
		Worktree: worktree,
	})
	if err != nil {
		return result, err
	}
	result.Session = launched.Session
	result.Label = launched.Caveat()
	return result, nil
}

// GroupRequest captures a group creation; the accepted effect validates the
// directory before the store write. Gen identifies the dispatching group form
// so a completion cannot replace a form the user has since reopened.
type GroupRequest struct {
	DraftName string
	DraftDir  string
	Path      string
	Dir       string
	Worktree  string
	Base      string
	Env       string
	Gen       int
	RawDir    string
	Fallbacks []string
	DirReader DirectoryPreflight
}

type GroupResult struct {
	Path     string
	Dir      string
	Worktree string
}

func RunGroup(services Services, request GroupRequest) (GroupResult, error) {
	dir := request.Dir
	if dir == "" {
		reader := request.DirReader
		if reader == nil {
			reader = SystemDirectoryPreflight{Git: services.Git}
		}
		var ok bool
		dir, ok = reader.Resolve(request.RawDir, request.Fallbacks)
		if !ok {
			return GroupResult{}, errors.New("default path does not exist: " + dir)
		}
	}
	if err := services.Store.AddGroup(request.Path, dir, request.Worktree, request.Base, request.Env); err != nil {
		return GroupResult{}, err
	}
	return GroupResult{Path: request.Path, Dir: dir, Worktree: request.Worktree}, nil
}

func SaveChoice(st SettingsStore, key, value string) error {
	if err := st.SetSetting(key, value); err != nil {
		return fmt.Errorf("saving %s: %w", key, err)
	}
	return nil
}

type SettingsStore interface {
	SetSetting(key, value string) error
}

// DirectoryPreflight is the read-only filesystem and repository seam a spawn
// or group creation captured before it was queued.
type DirectoryPreflight interface {
	Resolve(raw string, fallbacks []string) (string, bool)
	RepoCapable(dir string) bool
}

// SystemDirectoryPreflight resolves against the filesystem and asks git
// whether a directory sits inside a repository.
type SystemDirectoryPreflight struct{ Git *git.Driver }

func (p SystemDirectoryPreflight) Resolve(raw string, fallbacks []string) (string, bool) {
	if raw != "" {
		return ResolveExistingDir(raw, "")
	}
	var resolved string
	for _, fallback := range fallbacks {
		candidate, ok := ResolveExistingDir("", fallback)
		resolved = candidate
		if ok {
			return candidate, true
		}
	}
	return resolved, false
}

func (p SystemDirectoryPreflight) RepoCapable(dir string) bool {
	if p.Git == nil || dir == "" {
		return false
	}
	_, err := p.Git.RepoRoot(dir)
	return err == nil
}

// TerminalDirectoryRequest is where a shell tab or the editor opens: the pane
// whose live directory wins when it still answers, and the fallbacks.
type TerminalDirectoryRequest struct {
	PaneID    string
	RawDir    string
	Fallbacks []string
}

// TerminalDirectoryReader answers where a terminal opens.
type TerminalDirectoryReader interface {
	Resolve(request TerminalDirectoryRequest) (string, bool)
}

// SystemTerminalDirectoryReader answers from the pane's live directory, then
// the captured fallbacks.
type SystemTerminalDirectoryReader struct {
	// PanePath reads a pane's live directory; nil reads no pane.
	PanePath func(paneID string) (string, error)
	Dirs     DirectoryPreflight
}

// Resolve answers where the terminal opens.
func (r SystemTerminalDirectoryReader) Resolve(request TerminalDirectoryRequest) (string, bool) {
	if request.PaneID != "" && r.PanePath != nil {
		if path, err := r.PanePath(request.PaneID); err == nil && IsDir(path) {
			return path, true
		}
	}
	return r.Dirs.Resolve(request.RawDir, request.Fallbacks)
}

func InitialWorkingDir() string {
	dir, _ := os.Getwd()
	return dir
}

func InitialHomeDir() string {
	dir, _ := os.UserHomeDir()
	return dir
}

// ResolveExistingDir turns raw field input into a usable directory: expand
// ~, fall back when empty, absolutize, and require it to exist. The resolved
// value returns either way so error messages can show it.
func ResolveExistingDir(raw, fallback string) (string, bool) {
	dir := pathcomplete.ExpandHome(strings.TrimSpace(raw))
	if dir == "" {
		dir = fallback
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return dir, IsDir(dir)
}

func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
