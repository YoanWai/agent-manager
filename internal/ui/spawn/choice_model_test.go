package spawn

import "testing"

// The settings a form or a quick bar reads are the CLI's kept choice and
// the models it listed last, two keys per CLI.
func TestSettingKeysCoverEveryCLI(t *testing.T) {
	t.Parallel()
	got := SettingKeys([]string{"claude", "codex"})
	want := []string{
		savedChoiceKey("claude"), recentModelsKey("claude"),
		savedChoiceKey("codex"), recentModelsKey("codex"),
	}
	if len(got) != len(want) {
		t.Fatalf("keys = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %v, want %v", got, want)
		}
	}
}

// Only the stored "close" takes the bar down after a send.
func TestCloseAfterSendReadsTheStoredValue(t *testing.T) {
	t.Parallel()
	if !CloseAfterSend("close") {
		t.Fatal(`"close" should close the bar`)
	}
	for _, value := range []string{"", "keep", "stay"} {
		if CloseAfterSend(value) {
			t.Fatalf("%q should leave the bar up", value)
		}
	}
}
