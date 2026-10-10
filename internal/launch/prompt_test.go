package launch

import "testing"

// The launch notes are the manager's words, not a task: a decorated first
// prompt sheds them, and a note delivered on its own records nothing.
func TestTypedPromptStripsLaunchNotes(t *testing.T) {
	for _, note := range []string{ProactiveCoordinationNote, OnRequestCoordinationNote} {
		decorated := note + "\n\n" + RenameDirective + "\n\nfix the login flow"
		if got := DeliveredPrompt(decorated); got != "fix the login flow" {
			t.Fatalf("typedPrompt with note %q = %q, want the bare task", note, got)
		}
		if got := DeliveredPrompt(note); got != "" {
			t.Fatalf("a bare note should record nothing, got %q", got)
		}
	}
	if got := DeliveredPrompt(DeferredRenameDirective); got != "" {
		t.Fatalf("a bare directive should record nothing, got %q", got)
	}
	if got := DeliveredPrompt("plain prompt"); got != "plain prompt" {
		t.Fatalf("an undecorated prompt should pass through, got %q", got)
	}
}
