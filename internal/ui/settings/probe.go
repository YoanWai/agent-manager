package settings

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
