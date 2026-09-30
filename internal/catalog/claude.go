package catalog

import (
	"context"
	"encoding/json"
	"errors"
)

// readClaude reads the models off the Agent SDK's initialize response.
func readClaude(ctx context.Context, command, dir string) (Catalog, error) {
	proc, err := start(command, dir, false)
	if err != nil {
		return Catalog{}, err
	}
	defer proc.stop()
	const requestID = "catalog"
	request, err := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    map[string]string{"subtype": "initialize"},
	})
	if err != nil {
		return Catalog{}, err
	}
	if err := proc.send(request); err != nil {
		return Catalog{}, err
	}
	for {
		line, err := proc.next(ctx)
		if err != nil {
			return Catalog{}, err
		}
		var message struct {
			Type     string `json:"type"`
			Response struct {
				Subtype   string `json:"subtype"`
				RequestID string `json:"request_id"`
				Error     string `json:"error"`
				Response  struct {
					Models []struct {
						Value                 string   `json:"value"`
						DisplayName           string   `json:"displayName"`
						SupportedEffortLevels []string `json:"supportedEffortLevels"`
					} `json:"models"`
				} `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal(line, &message) != nil || message.Type != "control_response" || message.Response.RequestID != requestID {
			continue
		}
		if message.Response.Subtype != "success" {
			return Catalog{}, errors.New(message.Response.Error)
		}
		var cat Catalog
		for _, model := range message.Response.Response.Models {
			cat.Models = append(cat.Models, Model{
				ID:      model.Value,
				Label:   model.DisplayName,
				Efforts: model.SupportedEffortLevels,
				// The SDK lists the model a session starts on as "default".
				Default: model.Value == "default",
			})
		}
		return cat, nil
	}
}
