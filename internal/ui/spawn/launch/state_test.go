package launch

import (
	"testing"
	"time"
)

// A directory's git-repo answer is trusted inside the window and dropped
// after it, so a repo initialised while the quick bar is open is seen.
func TestWorktreeAnswerExpires(t *testing.T) {
	t.Parallel()
	state := State{}
	now := time.Now()
	state.RememberWorktree("/tmp/repo", true, now)

	if capable, known := state.CachedWorktree("/tmp/repo", now.Add(time.Second)); !known || !capable {
		t.Fatalf("fresh answer = %v, %v", capable, known)
	}
	if _, known := state.CachedWorktree("/tmp/repo", now.Add(WorktreeLookupTTL)); known {
		t.Fatal("an answer at the TTL boundary is stale")
	}
	state.ForgetWorktree()
	if _, known := state.CachedWorktree("/tmp/repo", now); known {
		t.Fatal("forgotten answer came back")
	}
}

// A burst of spawns into one repo runs a single base refresh.
func TestBaseFetchIsReservedOncePerInterval(t *testing.T) {
	t.Parallel()
	state := State{}
	key := BaseFetchKey{Dir: "/tmp/repo"}
	now := time.Now()

	if !state.ReserveBaseFetch(key, now) {
		t.Fatal("the first refresh was refused")
	}
	if state.ReserveBaseFetch(key, now.Add(time.Second)) {
		t.Fatal("a second refresh ran inside the interval")
	}
	if !state.ReserveBaseFetch(key, now.Add(BaseFetchInterval)) {
		t.Fatal("the interval did not expire")
	}
}

// A base picker steps through auto and the repo's branches, wrapping at both
// ends.
func TestStepBaseChoiceWraps(t *testing.T) {
	t.Parallel()
	refs := []string{"main", "dev"}
	cases := []struct {
		current string
		delta   int
		want    string
	}{
		{current: "", delta: -1, want: "dev"},
		{current: "", delta: 1, want: "main"},
		{current: "main", delta: 1, want: "dev"},
		{current: "dev", delta: 1, want: ""},
		{current: "main", delta: -1, want: ""},
	}
	for _, tc := range cases {
		if got := StepBaseChoice(refs, tc.current, tc.delta); got != tc.want {
			t.Fatalf("step(%q, %d) = %q, want %q", tc.current, tc.delta, got, tc.want)
		}
	}
}

// A repo's branches read inside the window step without asking git again.
func TestBaseRefsAreMemoized(t *testing.T) {
	t.Parallel()
	state := State{}
	now := time.Now()
	state.KeepBaseRefs("/tmp/repo", []string{"main"}, now)

	next, cmd, err := state.StepGroupBase(nil, BaseForm, 0, "/tmp/repo", "", 1, now.Add(time.Second))
	if err != nil || cmd != nil || next != "main" {
		t.Fatalf("memoized step = %q, %v, %v", next, cmd, err)
	}
	if next, _, err := state.StepGroupBase(nil, BaseForm, 0, "/tmp/repo", "", 1, now.Add(BaseRefsTTL)); err == nil || next != "" {
		t.Fatalf("stale answer with no git driver = %q, %v", next, err)
	}
}
