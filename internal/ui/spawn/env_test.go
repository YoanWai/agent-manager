package spawn

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestEnvPickFollowsTheGroupUntilStepped(t *testing.T) {
	var pick EnvPick
	pick.Seed([]string{"team", "work"}, "work")
	if pick.Name() != "work" || pick.Touched() {
		t.Fatalf("seeded pick = %q, touched %v", pick.Name(), pick.Touched())
	}
	pick.Default("team")
	if pick.Name() != "team" {
		t.Fatalf("an untouched pick did not follow the group: %q", pick.Name())
	}
	pick.Step(1)
	if pick.Name() != "work" || !pick.Touched() {
		t.Fatalf("stepped pick = %q", pick.Name())
	}
	pick.Default("")
	if pick.Name() != "work" {
		t.Fatalf("a stepped pick followed the group: %q", pick.Name())
	}
	pick.Step(1)
	if pick.Name() != "" {
		t.Fatalf("stepping past the last profile = %q, want none", pick.Name())
	}
	if pick.For("team") != "" {
		t.Fatal("a stepped none answered the group's default")
	}
}

func TestEnvPickShowsOnlyWithSomethingToPick(t *testing.T) {
	var pick EnvPick
	if pick.Shown() {
		t.Fatal("no profiles and no default still shows")
	}
	if !pick.ShownFor("gone") {
		t.Fatal("a group default naming a deleted profile is hidden")
	}
	pick.SetProfiles([]string{"work"})
	if !pick.Shown() {
		t.Fatal("a defined profile is hidden")
	}
}

func TestStepEnvStartsADeletedProfileFromNone(t *testing.T) {
	profiles := []string{"team", "work"}
	if got := StepEnv(profiles, "gone", 1); got != "team" {
		t.Fatalf("StepEnv from a deleted profile = %q, want team", got)
	}
	if got := StepEnv(profiles, "", -1); got != "work" {
		t.Fatalf("StepEnv back from none = %q, want work", got)
	}
}

// The env row steps with ←→ and a click, sits in the focus order only
// when there is something to pick, and rides the spawn it submits.
func TestFormEnvRowStepsAndRidesTheSpawn(t *testing.T) {
	h := newFakeFormHost()
	d := NewForm(h, []string{"plain"}, 0)
	if slices.Contains(d.Fields(h), FieldEnv) {
		t.Fatal("the env row shows with no profile defined")
	}
	d.Env().Seed([]string{"team", "work"}, "work")
	if !slices.Contains(d.Fields(h), FieldEnv) {
		t.Fatal("the env row is missing with profiles defined")
	}
	body, _ := d.View(h, FormFacts{Width: 60, Group: "root", Styles: Styles{Input: func(in textinput.Model) string { return in.View() }}, Chrome: func([][2]string) int { return 6 }, Height: 40})
	if plain := ansi.Strip(body); !strings.Contains(plain, "◂ work ▸  (group)") {
		t.Fatalf("the env row does not show the group default:\n%s", plain)
	}
	d.FocusField(h, FieldEnv)
	d.HandleKey(h, tea.KeyMsg{Type: tea.KeyLeft})
	if d.Env().Name() != "team" {
		t.Fatalf("left on the env row = %q, want team", d.Env().Name())
	}
	request := d.Submit(h)
	if request.Spawn == nil || request.Spawn.Env != "team" || !request.Spawn.EnvSet {
		t.Fatalf("submit = %+v, want env team", request.Spawn)
	}
}
