package onboarding

import (
	"slices"
	"testing"
)

func TestGuideStepsActOutEachLineInTurn(t *testing.T) {
	m := New(agents, defaults(), false)
	if !m.Animating() {
		t.Fatal("the welcome stands still")
	}
	walkTo(t, &m, Agents)
	if m.Animating() {
		t.Fatal("a choosing step animates")
	}
	walkTo(t, &m, QuickPrompt)
	if !m.Animating() {
		t.Fatal("the quick prompt step stands still")
	}
	var seen []int
	for range 120 {
		if line := m.line(); len(seen) == 0 || seen[len(seen)-1] != line {
			seen = append(seen, line)
		}
		m.Tick()
	}
	if want := []int{0, 1, 2, 1, 2, 1, 2, 0}; !slices.Equal(seen[:len(want)], want) {
		t.Fatalf("lines lit %v, want space, then type and enter for each of three agents, then again", seen)
	}
	m.Key(press("enter"))
	if scene, frame := m.scene(); scene != 0 || frame != 0 {
		t.Fatalf("the next step opened mid-scene %d, beat %d", scene, frame)
	}
	walkTo(t, &m, Places)
	if m.Animating() {
		t.Fatal("the last step animates")
	}
}

func TestClosedSetupStopsAnimating(t *testing.T) {
	m := New(agents, defaults(), false)
	walkTo(t, &m, Review)
	m.Key(press("esc"))
	m.Key(press("y"))
	if m.Animating() {
		t.Fatal("a closed setup still asks for ticks")
	}
}
