package store

import "testing"

func TestRemoteTerminalOpenerRoundTrip(t *testing.T) {
	st := newTestStore(t)
	if got, err := st.RemoteTerminalOpener("gpu", "t1"); err != nil || got != "" {
		t.Fatalf("unrecorded opener = %q, %v; want empty, nil", got, err)
	}
	if err := st.SetRemoteTerminalOpener("gpu", "t1", "a"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.RemoteTerminalOpener("gpu", "t1"); err != nil || got != "a" {
		t.Fatalf("opener = %q, %v; want a", got, err)
	}
	if got, _ := st.RemoteTerminalOpener("box", "t1"); got != "" {
		t.Fatalf("another host's t1 opener = %q, want empty", got)
	}
	if err := st.DeleteRemoteTerminal("gpu", "t1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.RemoteTerminalOpener("gpu", "t1"); got != "" {
		t.Fatalf("opener after delete = %q, want empty", got)
	}
}

// Session ids are recycled, so a deleted session's remote terminals must
// not pass to whichever session takes its id next.
func TestDeleteDropsRemoteTerminalOpeners(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g1")); err != nil {
		t.Fatal(err)
	}
	child := sample("sh", "g1")
	child.ParentID = "agent"
	if err := st.CreateSession(child); err != nil {
		t.Fatal(err)
	}
	for id, opener := range map[string]string{"t1": "agent", "t2": "sh", "t3": "other"} {
		if err := st.SetRemoteTerminalOpener("gpu", id, opener); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.DeleteChild("sh", "agent", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("agent"); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"t1": "", "t2": "", "t3": "other"} {
		if got, err := st.RemoteTerminalOpener("gpu", id); err != nil || got != want {
			t.Errorf("opener of %s = %q, %v; want %q", id, got, err, want)
		}
	}
}
