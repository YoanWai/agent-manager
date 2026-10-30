package spawn

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
)

// A field that offers no suggestions, a connection's directory, starts no
// read; one that does tags it with its own target.
func TestPathFieldSuggestHonoursOff(t *testing.T) {
	t.Parallel()
	off := PathField{State: &pathcomplete.State{}, Target: pathcomplete.Form, Off: true}
	if cmd := off.Suggest("/tmp"); cmd != nil {
		t.Fatal("a field with no suggestions started a read")
	}
	on := PathField{State: &pathcomplete.State{}, Target: pathcomplete.Form}
	if cmd := on.Suggest("/tmp"); cmd == nil {
		t.Fatal("a field with suggestions started no read")
	}
}

// The hint names the create key only while nothing is highlighted.
func TestPathHintFollowsTheHighlight(t *testing.T) {
	t.Parallel()
	plain := PathHint(false)
	chosen := PathHint(true)
	if plain[2] != [2]string{"↵", "create"} {
		t.Fatalf("unhighlighted hint = %v, want enter to create", plain)
	}
	if chosen[1] != [2]string{"↵/tab", "complete"} {
		t.Fatalf("highlighted hint = %v, want enter to complete", chosen)
	}
}
