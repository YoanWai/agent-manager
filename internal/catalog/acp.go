package catalog

import "context"

// acpOption is a session config option. Its choices come flat or in named
// groups; both are the protocol's shape.
type acpOption struct {
	ID           string      `json:"id"`
	Category     string      `json:"category"`
	CurrentValue string      `json:"currentValue"`
	Options      []acpChoice `json:"options"`
}

type acpChoice struct {
	Value   string      `json:"value"`
	Name    string      `json:"name"`
	Options []acpChoice `json:"options"`
}

func (o acpOption) choices() []acpChoice {
	var flat []acpChoice
	for _, choice := range o.Options {
		if choice.Value != "" {
			flat = append(flat, choice)
		}
		flat = append(flat, choice.Options...)
	}
	return flat
}

func optionIn(options []acpOption, category string) (acpOption, bool) {
	for _, option := range options {
		if option.Category == category {
			return option, true
		}
	}
	return acpOption{}, false
}

// readACP opens an Agent Client Protocol session and reads its model
// option; where the agent has a thought level option, each model is set in
// turn to read the levels that model takes. Agents that predate config
// options list their models under the session's models state instead.
func readACP(ctx context.Context, command, dir string) (Catalog, error) {
	proc, err := start(command, dir, false)
	if err != nil {
		return Catalog{}, err
	}
	defer proc.stop()
	client := &rpcClient{proc: proc}
	var initialized struct{}
	if err := client.call(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false},
		"clientInfo":         clientInfo,
	}, &initialized); err != nil {
		return Catalog{}, err
	}
	var session struct {
		SessionID     string      `json:"sessionId"`
		ConfigOptions []acpOption `json:"configOptions"`
		Models        *struct {
			CurrentModelID  string `json:"currentModelId"`
			AvailableModels []struct {
				ModelID string `json:"modelId"`
				Name    string `json:"name"`
			} `json:"availableModels"`
		} `json:"models"`
	}
	if err := client.call(ctx, "session/new", map[string]any{"cwd": dir, "mcpServers": []any{}}, &session); err != nil {
		return Catalog{}, err
	}
	modelOption, hasModelOption := optionIn(session.ConfigOptions, "model")
	if !hasModelOption {
		var cat Catalog
		if session.Models != nil {
			for _, model := range session.Models.AvailableModels {
				cat.Models = append(cat.Models, Model{ID: model.ModelID, Label: model.Name, Default: model.ModelID == session.Models.CurrentModelID})
			}
		}
		return cat, nil
	}
	_, hasThought := optionIn(session.ConfigOptions, "thought_level")
	var cat Catalog
	for _, choice := range modelOption.choices() {
		model := Model{ID: choice.Value, Label: choice.Name, Default: choice.Value == modelOption.CurrentValue}
		if hasThought {
			var set struct {
				ConfigOptions []acpOption `json:"configOptions"`
			}
			if err := client.call(ctx, "session/set_config_option", map[string]any{
				"sessionId": session.SessionID, "configId": modelOption.ID, "value": choice.Value,
			}, &set); err != nil {
				return Catalog{}, err
			}
			if thought, ok := optionIn(set.ConfigOptions, "thought_level"); ok {
				for _, level := range thought.choices() {
					model.Efforts = append(model.Efforts, level.Value)
					// grok keeps the session's level as current even for a
					// model that does not offer it.
					if level.Value == thought.CurrentValue {
						model.DefaultEffort = level.Value
					}
				}
			}
		}
		cat.Models = append(cat.Models, model)
	}
	return cat, nil
}
