package settings

import "slices"

// Spec is one key Settings writes and the values it takes; the root hands
// the list to the shell as store.SettingSpec.
type Spec struct {
	Key     string
	Values  []string
	Default string
	// List marks a key holding a comma-separated subset of Values.
	List bool
	// Row is the label of the Settings row that steps this key, empty for
	// a key the shell alone sets.
	Row string
}

// Specs is every key Settings writes, so the shell checks a value against
// the same choices the modal steps through. tools are the agent CLIs.
func Specs(tools []string) []Spec {
	specs := make([]Spec, len(table))
	for i, setting := range table {
		spec := setting.Spec
		switch spec.Key {
		case DefaultToolSetting, HiddenToolsSetting:
			spec.Values = tools
		default:
			spec.Values = slices.Clone(spec.Values)
		}
		specs[i] = spec
	}
	return specs
}
