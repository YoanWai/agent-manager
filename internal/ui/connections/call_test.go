package connections

import (
	"fmt"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

// A remote row's action either names the call it makes, the question it asks
// first, or the reason it stays here; a local action is left alone.
func TestSessionCallAnswersEveryRemoteAction(t *testing.T) {
	row := Row{Kind: SessionRow, Host: "box", Name: "api", Status: "idle"}
	cases := []struct {
		name string
		row  Row
		kind uirail.ActionKind
		want Call
	}{
		{"attach", row, uirail.Focus, Call{Op: OpAttach, Answered: true}},
		{"kill asks with the RAM consequence", row, uirail.Kill,
			Call{Op: OpKill, Answered: true, Action: uidialogs.ConfirmKill, Verb: "kill", Consequence: " frees its RAM there, v revives it."}},
		{"a dead kill drops the consequence", Row{Kind: SessionRow, Host: "box", Name: "api", Status: "dead"}, uirail.Kill,
			Call{Op: OpKill, Dead: true, Answered: true, Action: uidialogs.ConfirmKill, Verb: "kill"}},
		{"a terminal kill closes it", Row{Kind: SessionRow, Host: "box", Name: "sh", Terminal: true}, uirail.Kill,
			Call{Op: OpCloseTerminal, Answered: true, Action: uidialogs.ConfirmKill, Verb: "close terminal"}},
		{"revive", row, uirail.Revive,
			Call{Op: OpRevive, Answered: true, Action: uidialogs.ConfirmRevive, Verb: "revive"}},
		{"archive an unarchived row", row, uirail.Archive,
			Call{Op: OpArchive, Answered: true, Action: uidialogs.ConfirmArchive, Verb: "archive"}},
		{"restore an archived row", Row{Kind: SessionRow, Host: "box", Name: "api", Archived: true}, uirail.Restore,
			Call{Op: OpRestore, Answered: true, Action: uidialogs.ConfirmRestore, Verb: "restore"}},
		{"archive an archived row is a no-op", Row{Kind: SessionRow, Host: "box", Name: "api", Archived: true}, uirail.Archive, Call{Answered: true}},
		{"restore an unarchived row is a no-op", row, uirail.Restore, Call{Answered: true}},
		{"a dead attach names the revive key", Row{Kind: SessionRow, Host: "box", Name: "api", Status: "dead"}, uirail.Attach,
			Call{Answered: true, Refusal: "api is dead on box - press v to revive it"}},
		{"a terminal says how to close it", Row{Kind: SessionRow, Host: "box", Name: "sh", Status: "dead", Terminal: true}, uirail.Focus,
			Call{Answered: true, Refusal: "terminal sh has exited on box - x closes it"}},
		{"a terminal is not archived", Row{Kind: SessionRow, Host: "box", Name: "sh", Terminal: true}, uirail.Archive,
			Call{Answered: true, Refusal: "a terminal on box is not archived or revived - x closes it"}},
		{"a local action stays local", row, uirail.OpenReview, Call{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SessionCall(tc.row, tc.kind, boundKeys(map[string]string{keybind.Revive: "v", keybind.Kill: "x"})); got != tc.want {
				t.Fatalf("SessionCall = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSessionCallNamesTheBoundKeys(t *testing.T) {
	row := Row{Kind: SessionRow, Host: "box", Name: "api", Status: "idle"}
	dead := Row{Kind: SessionRow, Host: "box", Name: "api", Status: "dead"}
	shell := Row{Kind: SessionRow, Host: "box", Name: "sh", Terminal: true}
	cases := []struct {
		name string
		keys map[string]string
		row  Row
		kind uirail.ActionKind
		want string
	}{
		{"rebound revive on a kill", map[string]string{keybind.Revive: "Z"}, row, uirail.Kill, " frees its RAM there, Z revives it."},
		{"unbound revive on a kill", nil, row, uirail.Kill, " frees its RAM there."},
		{"rebound revive on a dead attach", map[string]string{keybind.Revive: "Z"}, dead, uirail.Attach, "api is dead on box - press Z to revive it"},
		{"unbound revive on a dead attach", nil, dead, uirail.Attach, "api is dead on box"},
		{"rebound kill on a terminal", map[string]string{keybind.Kill: "D"}, shell, uirail.Archive, "a terminal on box is not archived or revived - D closes it"},
		{"unbound kill on a terminal", nil, shell, uirail.Archive, "a terminal on box is not archived or revived"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call := SessionCall(tc.row, tc.kind, boundKeys(tc.keys))
			if got := call.Consequence + call.Refusal; got != tc.want {
				t.Fatalf("SessionCall words it %q, want %q", got, tc.want)
			}
		})
	}
}

// boundKeys is a key hint over the keys a test binds; an action it leaves out
// has no key.
func boundKeys(bound map[string]string) KeyHint {
	return func(action, format string) string {
		if bound[action] == "" {
			return ""
		}
		return fmt.Sprintf(format, bound[action])
	}
}

// A prompt aimed at a remote row refuses to land when a refresh moved the
// target, and a local-to-local aim is the root's own business.
func TestAimLostNamesTheReasonTheAimCannotLand(t *testing.T) {
	state := &Connections{list: nil, hosts: map[string]Host{}, polling: map[string]bool{}}
	aim := uirail.Selection{Kind: uirail.SessionRow, Host: "box", SessionID: "s1"}
	if _, lost := state.AimLost(uirail.Selection{Kind: uirail.SessionRow, SessionID: "local"}, uirail.Selection{Kind: uirail.SessionRow, SessionID: "other"}); lost {
		t.Fatal("a local-to-local aim is the root's to refuse")
	}
	if refusal, lost := state.AimLost(aim, aim); !lost || refusal == "" {
		t.Fatal("a row the last snapshot no longer lists must refuse")
	}
	state.hosts["box"] = Host{Sessions: []Session{{ID: "s1", Name: "api"}}}
	if refusal, lost := state.AimLost(aim, aim); lost {
		t.Fatalf("the aim still lands on its row: %q", refusal)
	}
	if refusal, lost := state.AimLost(aim, uirail.Selection{Kind: uirail.SessionRow, Host: "box", SessionID: "s2"}); !lost ||
		refusal != "the selection moved since this prompt was aimed; nothing was sent" {
		t.Fatalf("a moved cursor = %q, %t", refusal, lost)
	}
	// A session that changed group is still the same session.
	moved := aim
	moved.Group = "web"
	if _, lost := state.AimLost(aim, moved); lost {
		t.Fatal("a session that changed group is the same row")
	}
}
