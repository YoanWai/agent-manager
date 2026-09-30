package launch

import "testing"

// The launch notes are the manager's words, not a task: a decorated first
// prompt sheds them, and a note delivered on its own records nothing.
func TestTypedPromptStripsLaunchNotes(t *testing.T) {
	decorated := CoordinationNote + "\n\n" + RenameDirective + "\n\nfix the login flow"
	if got := DeliveredPrompt(decorated); got != "fix the login flow" {
		t.Fatalf("typedPrompt = %q, want the bare task", got)
	}
	if got := DeliveredPrompt(DeferredRenameDirective); got != "" {
		t.Fatalf("a bare directive should record nothing, got %q", got)
	}
	if got := DeliveredPrompt(CoordinationNote); got != "" {
		t.Fatalf("a bare note should record nothing, got %q", got)
	}
	if got := DeliveredPrompt("plain prompt"); got != "plain prompt" {
		t.Fatalf("an undecorated prompt should pass through, got %q", got)
	}
}
