package spawn

import (
	"slices"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

// EnvPick is the environment profile a spawn launches under: one of the
// defined profiles or none. It stands on the group's default until the user
// steps it. The form keeps that default here; the quick bar, whose target
// follows the cursor, passes its target's to the *For calls.
type EnvPick struct {
	profiles []string
	def      string
	name     string
	touched  bool
}

// Seed starts the pick on the group's default among profiles.
func (e *EnvPick) Seed(profiles []string, groupDefault string) {
	*e = EnvPick{profiles: profiles, def: groupDefault}
}

// SetProfiles takes a newer list of defined profiles, keeping the pick.
func (e *EnvPick) SetProfiles(profiles []string) { e.profiles = profiles }

// Default moves the group default a pick the user has not stepped stands on.
func (e *EnvPick) Default(groupDefault string) { e.def = groupDefault }

// Touched says the user stepped the pick, which defaults then leave be.
func (e EnvPick) Touched() bool { return e.touched }

// Name is the picked profile, "" for none.
func (e EnvPick) Name() string { return e.For(e.def) }

// For is the picked profile when the group's default is groupDefault.
func (e EnvPick) For(groupDefault string) string {
	if e.touched {
		return e.name
	}
	return groupDefault
}

// Shown says there is anything to pick, or a default to show.
func (e EnvPick) Shown() bool { return e.ShownFor(e.def) }

func (e EnvPick) ShownFor(groupDefault string) bool {
	return len(e.profiles) > 0 || e.For(groupDefault) != ""
}

// Step moves the pick through none and the defined profiles.
func (e *EnvPick) Step(delta int) { e.StepFor(e.def, delta) }

func (e *EnvPick) StepFor(groupDefault string, delta int) {
	e.name = StepEnv(e.profiles, e.For(groupDefault), delta)
	e.touched = true
}

// Label paints the pick, naming a default the group gave it.
func (e EnvPick) Label(st Styles) string {
	name := e.Name()
	value := st.Subtle.Render("none")
	if name != "" {
		value = st.Value.Render(name)
	}
	chrome := presentation.ChromeFor(st.GlyphMode)
	label := st.Subtle.Render(chrome.ChoiceLeftGlyph+" ") + value + st.Subtle.Render(" "+chrome.ChoiceRightGlyph)
	if name != "" && !e.touched {
		label += st.Subtle.Render("  (group)")
	}
	return label
}

// StepEnv moves an environment choice through none and profiles; a
// choice naming a profile since deleted steps from none.
func StepEnv(profiles []string, current string, delta int) string {
	choices := append([]string{""}, profiles...)
	at := max(slices.Index(choices, current), 0)
	return choices[(at+delta+len(choices))%len(choices)]
}
