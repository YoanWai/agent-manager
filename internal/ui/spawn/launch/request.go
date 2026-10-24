package launch

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
)

// Defaults are the launch inputs a spawn into a group captures at dispatch
// time: the ref a worktree branches from, the pane size, the group's own
// directory and its ancestors', and the worktree default in force.
type Defaults struct {
	Base     string
	Pane     sessioncmd.PaneSize
	GroupDir string
	// Fallbacks are the group's own path, then its ancestors', then the
	// directory the manager started in.
	Fallbacks []string
	Worktree  bool
}

// FormRequest is the spawn a New Session form dispatches. name is the
// session's name, empty for one the manager generates; prompt is the form's
// prompt box, which the request takes the images out of.
func FormRequest(request spawn.Request, name, group string, prompt *composer.Composer, defaults Defaults) Request {
	autoNamed := name == ""
	if autoNamed {
		name = request.Tool + "-" + nameSuffix()
	}
	return Request{
		Kind:         Form,
		ToolName:     request.Tool,
		Name:         name,
		Group:        group,
		Prompt:       launch.WithInstructions(request.Saved, request.Prompt),
		AutoNamed:    autoNamed,
		PickWorktree: request.Worktree,
		Choice:       config.Choice(request.Pick),
		Base:         defaults.Base,
		Pane:         defaults.Pane,
		ComposerGen:  prompt.Gen,
		Images:       prompt.Attachments,
		Draft:        prompt.Input.Value(),
		RawDir:       request.Dir,
		DirFallbacks: defaults.Fallbacks,
		WantWorktree: request.Worktree,
	}
}

// QuickRequest is the spawn the quick bar dispatches: it names its own
// session and leaves the directory to the group.
func QuickRequest(request spawn.Request, prompt *composer.Composer, defaults Defaults) Request {
	return Request{
		Kind:         Quick,
		ToolName:     request.Tool,
		Name:         request.Tool + "-" + nameSuffix(),
		Group:        request.Group,
		Prompt:       request.Prompt,
		AutoNamed:    true,
		PickWorktree: request.Worktree,
		Choice:       config.Choice(request.Pick),
		Base:         defaults.Base,
		Pane:         defaults.Pane,
		ComposerGen:  prompt.Gen,
		Images:       prompt.Attachments,
		Draft:        prompt.Input.Value(),
		RawDir:       defaults.GroupDir,
		DirFallbacks: defaults.Fallbacks,
		WantWorktree: request.Worktree,
	}
}

// nameSuffix marks a generated session name as this launch's.
func nameSuffix() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().Format("150405")
	}
	return hex.EncodeToString(buf)[:4]
}
