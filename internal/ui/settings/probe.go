package settings

import (
	"slices"
	"strings"
)

// PromptRows is the saved prompts catalog as the panel lists it.
func PromptRows(rows []Preset) []Prompt {
	prompts := make([]Prompt, len(rows))
	for i, row := range rows {
		prompts[i] = Prompt{Name: row.Name, Text: row.Text}
	}
	return prompts
}

// Preset is one stored prompt as the catalog read returns it, so this
// package shapes rows without importing the store.
type Preset struct {
	Name string
	Text string
}

// EditorProbe is the editors a machine offers, from the environment the
// root captured. Lookup walks PATH, which the update path must not do, so
// Resolve runs off it.
type EditorProbe struct {
	GUI    []string
	Visual string
	Editor string
	Lookup func(string) (string, error)
	// Line is the root's own resolution of the launch line, which the row
	// falls back to when nothing is configured.
	Line func() string
}

// Resolve answers the probe for the editor row.
func (p EditorProbe) Resolve() EditorsProbe {
	var found []string
	if p.Lookup != nil {
		for _, name := range p.GUI {
			if _, err := p.Lookup(name); err == nil {
				found = append(found, name)
			}
		}
	}
	for _, line := range []string{p.Visual, p.Editor} {
		if line = strings.TrimSpace(line); line != "" && !slices.Contains(found, line) {
			found = append(found, line)
		}
	}
	return EditorsProbe{Found: found, Auto: p.Line()}
}
