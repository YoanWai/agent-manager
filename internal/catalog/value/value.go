// Package value holds what a CLI answered about its models, efforts and
// profiles, without the processes that ask it.
package value

// Model is one model a CLI offers.
type Model struct {
	ID string `json:"id"`
	// Provider routes ID for a CLI that picks models per provider.
	Provider string `json:"provider,omitempty"`
	Label    string `json:"label,omitempty"`
	// Efforts are the reasoning levels the model takes, in the CLI's order.
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
	// EffortTyped marks a model that reasons without listed levels.
	EffortTyped bool `json:"effort_typed,omitempty"`
	// Default is the model the CLI starts on when no model is given.
	Default bool `json:"default,omitempty"`
}

// Key is the id, prefixed with its provider where the CLI routes by one.
func (m Model) Key() string {
	if m.Provider == "" {
		return m.ID
	}
	return m.Provider + ":" + m.ID
}

// Profile is a named setup a CLI launches under, with the models it offers.
type Profile struct {
	Name   string  `json:"name"`
	Detail string  `json:"detail,omitempty"`
	Models []Model `json:"models,omitempty"`
}

// Catalog is what one CLI answered.
type Catalog struct {
	Models   []Model   `json:"models"`
	Profiles []Profile `json:"profiles,omitempty"`
}

// ModelsFor is profile's models, the CLI's own for the empty profile.
func (c Catalog) ModelsFor(profile string) []Model {
	for _, p := range c.Profiles {
		if p.Name == profile {
			return p.Models
		}
	}
	return c.Models
}

// Match returns the model whose Key is key, else every model with that id.
func Match(models []Model, key string) []Model {
	var byID []Model
	for _, model := range models {
		if model.Key() == key {
			return []Model{model}
		}
		if model.ID == key {
			byID = append(byID, model)
		}
	}
	return byID
}

// Default is the model the CLI starts on when none is given, if it says.
func Default(models []Model) (Model, bool) {
	for _, model := range models {
		if model.Default {
			return model, true
		}
	}
	return Model{}, false
}
