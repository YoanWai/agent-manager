package launch

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/charmbracelet/bubbles/textarea"
)

func promptWith(text string, gen int) *composer.Composer {
	c := &composer.Composer{Input: textarea.New(), Gen: gen}
	c.Input.SetValue(text)
	c.Attachments = []composer.Attachment{{ID: 1, Path: "/tmp/a.png"}}
	return c
}

// A form spawn names itself when it carries no name, takes the form's prompt
// draft and images, and keeps the group's launch inputs.
func TestFormRequestCapturesTheDraft(t *testing.T) {
	t.Parallel()
	defaults := Defaults{
		Base:      "main",
		Pane:      sessioncmd.PaneSize{Width: 80, Height: 24},
		GroupDir:  "/repo",
		Fallbacks: []string{"/repo", "/"},
	}
	request := FormRequest(
		spawn.Request{Tool: "claude", Prompt: "fix it", Saved: "rules", Dir: "/repo/wt"},
		"", "web", promptWith("fix it", 7), defaults,
	)

	if !request.AutoNamed || !strings.HasPrefix(request.Name, "claude-") {
		t.Fatalf("name = %q auto = %v, want a generated claude name", request.Name, request.AutoNamed)
	}
	if request.Kind != Form || request.ComposerGen != 7 || request.Draft != "fix it" {
		t.Fatalf("request = %+v, want the form's prompt draft", request)
	}
	if len(request.Images) != 1 || request.RawDir != "/repo/wt" || request.Base != "main" {
		t.Fatalf("request = %+v, want the images and the captured inputs", request)
	}
	if request.Choice != config.Choice(spawn.Pick{}) {
		t.Fatalf("choice = %+v, want the CLI's defaults", request.Choice)
	}
	if !request.WantWorktree && !request.PickWorktree {
		return
	}
	t.Fatalf("request = %+v, want no worktree unless asked", request)
}

// A named form keeps the name the user typed.
func TestFormRequestKeepsATypedName(t *testing.T) {
	t.Parallel()
	request := FormRequest(spawn.Request{Tool: "claude"}, "job", "web", promptWith("", 1), Defaults{})
	if request.AutoNamed || request.Name != "job" {
		t.Fatalf("name = %q auto = %v, want the typed name", request.Name, request.AutoNamed)
	}
}

// A quick spawn always names itself and leaves the directory to the group.
func TestQuickRequestLeavesTheDirectoryToTheGroup(t *testing.T) {
	t.Parallel()
	request := QuickRequest(spawn.Request{Tool: "codex", Group: "web", Prompt: "ship"}, promptWith("ship", 3), Defaults{GroupDir: "/repo"})

	if request.Kind != Quick || !request.AutoNamed || !strings.HasPrefix(request.Name, "codex-") {
		t.Fatalf("request = %+v, want an auto-named quick spawn", request)
	}
	if request.RawDir != "/repo" || request.Group != "web" || request.ComposerGen != 3 {
		t.Fatalf("request = %+v, want the group's directory and the prompt's draft", request)
	}
}
