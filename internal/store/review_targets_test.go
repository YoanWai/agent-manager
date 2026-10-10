package store

import "testing"

// Deleting a session must take its review target with it, or a recycled
// session id would inherit a dead repo declaration.
func TestDeleteDropsReviewTarget(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("a", "g1")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReviewRepo("a", "/repos/alpha"); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ReviewRepo("a"); err != nil || got != "" {
		t.Fatalf("review target survived the delete: %q, %v", got, err)
	}
}

func TestReviewRepoRoundTrip(t *testing.T) {
	st := newTestStore(t)
	if got, err := st.ReviewRepo("s1"); err != nil || got != "" {
		t.Fatalf("unset review repo = %q, %v; want empty, nil", got, err)
	}
	if err := st.SetReviewRepo("s1", "/repos/alpha"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ReviewRepo("s1"); err != nil || got != "/repos/alpha" {
		t.Fatalf("review repo = %q, %v; want /repos/alpha", got, err)
	}
	if err := st.SetReviewRepo("s1", "/repos/bravo"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewRepo("s1"); got != "/repos/bravo" {
		t.Fatalf("review repo after update = %q, want /repos/bravo", got)
	}
	if err := st.SetReviewRepo("s1", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewRepo("s1"); got != "" {
		t.Fatalf("review repo after clear = %q, want empty", got)
	}
}

// Deleting a session must take its review bases with it, or a recycled
// session id would inherit a dead base declaration.
func TestDeleteDropsReviewBase(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("a", "g1")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReviewBase("a", "/repos/alpha", "main"); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ReviewBase("a", "/repos/alpha"); err != nil || got != "" {
		t.Fatalf("review base survived the delete: %q, %v", got, err)
	}
}

func TestReviewBaseRoundTrip(t *testing.T) {
	st := newTestStore(t)
	if got, err := st.ReviewBase("s1", "/repos/alpha"); err != nil || got != "" {
		t.Fatalf("unset review base = %q, %v; want empty, nil", got, err)
	}
	if err := st.SetReviewBase("s1", "/repos/alpha", "main"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ReviewBase("s1", "/repos/alpha"); err != nil || got != "main" {
		t.Fatalf("review base = %q, %v; want main", got, err)
	}
	// A second repo under the same session keeps its own base.
	if err := st.SetReviewBase("s1", "/repos/bravo", "develop"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewBase("s1", "/repos/bravo"); got != "develop" {
		t.Fatalf("bravo base = %q, want develop", got)
	}
	if got, _ := st.ReviewBase("s1", "/repos/alpha"); got != "main" {
		t.Fatalf("alpha base after bravo set = %q, want main", got)
	}
	if err := st.SetReviewBase("s1", "/repos/alpha", "master"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewBase("s1", "/repos/alpha"); got != "master" {
		t.Fatalf("alpha base after update = %q, want master", got)
	}
	if err := st.SetReviewBase("s1", "/repos/alpha", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewBase("s1", "/repos/alpha"); got != "" {
		t.Fatalf("alpha base after clear = %q, want empty", got)
	}
}
