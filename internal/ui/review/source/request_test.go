package source

import (
	"testing"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
)

func TestRouteNamesEveryReviewMessage(t *testing.T) {
	for _, tc := range []struct {
		msg  any
		want Message
	}{
		{PickerLoadedMsg{}, MessagePickerLoaded},
		{uireview.PickedMsg{}, MessagePicked},
		{PreferencesMsg{}, MessagePreferences},
		{LoadMsg{}, MessageLoad},
		{uireview.FileResult{}, MessageFile},
		{FilesResult{}, MessageFiles},
		{uireview.HighlightResult{}, MessageHighlight},
		{uireview.ProbeResult{}, MessageProbe},
		{uireview.StatusResult{}, MessageStatus},
		{uireview.SaveResult{}, MessageSave},
		{uireview.HandleCommentResult{}, MessageHandle},
		{uireview.SendResult{}, MessageSend},
	} {
		got, ok := Route(tc.msg)
		if !ok || got != tc.want {
			t.Errorf("%T routed to %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestRouteLeavesOtherMessagesAlone(t *testing.T) {
	var messages []any
	messages = append(messages, uieffects.Completed{}, "text")
	for _, msg := range messages {
		if got, ok := Route(msg); ok {
			t.Errorf("%T claimed as %v", msg, got)
		}
	}
}

func TestChangeCopiesWhatTheCallerStillHolds(t *testing.T) {
	original := Change{Request: Request{
		Op:         Save,
		State:      uireview.SavedState{Reviewed: map[string]uint64{"a.go": 1}, Comments: []uireview.Comment{{ID: "c1"}}},
		CommentIDs: []string{"c1"},
	}}
	copied := original.EffectCopy()
	original.State.Reviewed["a.go"] = 2
	original.State.Comments[0].ID = "changed"
	original.CommentIDs[0] = "changed"

	got := copied.(Change)
	if got.State.Reviewed["a.go"] != 1 {
		t.Fatalf("copied mark = %d, want 1", got.State.Reviewed["a.go"])
	}
	if got.State.Comments[0].ID != "c1" || got.CommentIDs[0] != "c1" {
		t.Fatalf("copied slices follow the caller: %+v %v", got.State.Comments, got.CommentIDs)
	}
	if got.EffectName() == "" {
		t.Fatal("a change names itself with nothing")
	}
}

func TestOnlyASendFencesAPoll(t *testing.T) {
	for _, op := range []Op{Save, Handle, Status, Normalize, SetBase} {
		if IsSend(Change{Request: Request{Op: op}}) {
			t.Errorf("op %d was treated as a send", op)
		}
	}
	if !IsSend(Change{Request: Request{Op: Send}}) {
		t.Fatal("a send did not fence the poll")
	}
	if IsSend(Change{}) {
		t.Fatal("an empty request was treated as a send")
	}
}

func TestPendingSetBaseReadsRunningAndQueuedJobs(t *testing.T) {
	other := &uieffects.Job{Request: Change{Request: Request{Op: Save}}}
	base := &uieffects.Job{Request: Change{Request: Request{Op: SetBase}}}
	if PendingSetBase(nil, other) {
		t.Fatal("a save was read as a base save")
	}
	if !PendingSetBase(nil, other, base) {
		t.Fatal("a queued base save was not seen")
	}
	if !PendingSetBase(base) {
		t.Fatal("a running base save was not seen")
	}
}
