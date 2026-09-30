package catalog

import "context"

// readCodex asks codex's app server for model/list, where each model carries
// its effort levels and the one it defaults to.
func readCodex(ctx context.Context, command, dir string) (Catalog, error) {
	proc, err := start(command, dir, false)
	if err != nil {
		return Catalog{}, err
	}
	defer proc.stop()
	client := &rpcClient{proc: proc}
	var initialized struct{}
	if err := client.call(ctx, "initialize", map[string]any{"clientInfo": clientInfo}, &initialized); err != nil {
		return Catalog{}, err
	}
	if err := client.notify("initialized", nil); err != nil {
		return Catalog{}, err
	}
	var cat Catalog
	params := map[string]any{}
	for {
		var page struct {
			Data []struct {
				Model                     string `json:"model"`
				DisplayName               string `json:"displayName"`
				Hidden                    bool   `json:"hidden"`
				IsDefault                 bool   `json:"isDefault"`
				DefaultReasoningEffort    string `json:"defaultReasoningEffort"`
				SupportedReasoningEfforts []struct {
					ReasoningEffort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := client.call(ctx, "model/list", params, &page); err != nil {
			return Catalog{}, err
		}
		for _, model := range page.Data {
			if model.Hidden {
				continue
			}
			entry := Model{ID: model.Model, Label: model.DisplayName, DefaultEffort: model.DefaultReasoningEffort, Default: model.IsDefault}
			for _, effort := range model.SupportedReasoningEfforts {
				entry.Efforts = append(entry.Efforts, effort.ReasoningEffort)
			}
			cat.Models = append(cat.Models, entry)
		}
		if page.NextCursor == nil {
			return cat, nil
		}
		params = map[string]any{"cursor": *page.NextCursor}
	}
}
