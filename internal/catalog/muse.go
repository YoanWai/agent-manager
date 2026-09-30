package catalog

import (
	"context"
	"encoding/json"
)

// readMuse asks muse serve for model/list, where each model carries its
// complete effort set, or "unknown" when the provider does not say.
func readMuse(ctx context.Context, command, dir string) (Catalog, error) {
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
	var listed struct {
		Models []struct {
			ModelID      string          `json:"modelId"`
			DisplayLabel string          `json:"displayLabel"`
			IsDefault    bool            `json:"isDefault"`
			Variants     json.RawMessage `json:"variants"`
		} `json:"models"`
	}
	if err := client.call(ctx, "model/list", map[string]any{}, &listed); err != nil {
		return Catalog{}, err
	}
	var cat Catalog
	for _, model := range listed.Models {
		entry := Model{ID: model.ModelID, Label: model.DisplayLabel, Default: model.IsDefault}
		// A known set is an array; the only other shape is "unknown".
		if len(model.Variants) > 0 && model.Variants[0] == '[' {
			if err := json.Unmarshal(model.Variants, &entry.Efforts); err != nil {
				return Catalog{}, err
			}
		}
		cat.Models = append(cat.Models, entry)
	}
	return cat, nil
}
