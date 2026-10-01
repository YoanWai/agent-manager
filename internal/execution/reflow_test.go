package execution

import "testing"

func TestStoppedReflowReportsUnexecutedWork(t *testing.T) {
	runner := &Runner{stopped: true}
	ran := false
	if err := runner.ReflowSessions(nil, func() { ran = true }); err == nil {
		t.Fatal("stopped reflow reported success")
	}
	if ran {
		t.Fatal("stopped runner executed work")
	}
}
func TestEmptyReflowStillExecutesGroupWork(t *testing.T) {
	runner := &Runner{}
	ran := false
	if err := runner.ReflowSessions(nil, func() { ran = true }); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("empty group work was skipped")
	}
}
