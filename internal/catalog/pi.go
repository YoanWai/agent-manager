package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/YoanWai/agent-manager/internal/update"
)

// piKeepsDefaults is the first pi whose set_model stays in the session.
// Earlier releases save every model they are set to as the user's default.
const piKeepsDefaults = "0.84.3"

type piModel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

type piClient struct {
	proc   *process
	lastID int
}

func (c *piClient) call(ctx context.Context, command map[string]any, out any) error {
	c.lastID++
	id := strconv.Itoa(c.lastID)
	command["id"] = id
	line, err := json.Marshal(command)
	if err != nil {
		return err
	}
	if err := c.proc.send(line); err != nil {
		return err
	}
	for {
		line, err := c.proc.next(ctx)
		if err != nil {
			return err
		}
		var response struct {
			ID      string          `json:"id"`
			Type    string          `json:"type"`
			Success bool            `json:"success"`
			Error   string          `json:"error"`
			Data    json.RawMessage `json:"data"`
		}
		if json.Unmarshal(line, &response) != nil || response.Type != "response" || response.ID != id {
			continue
		}
		if !response.Success {
			return errors.New(response.Error)
		}
		return json.Unmarshal(response.Data, out)
	}
}

func readPi(ctx context.Context, command, dir string) (Catalog, error) {
	if err := checkPiVersion(ctx, command, dir); err != nil {
		return Catalog{}, err
	}
	return readPiRPC(ctx, command, dir)
}

// readOmp skips pi's version check, which cannot read omp's "omp/18.4.8";
// omp's set_model already stays in the session.
func readOmp(ctx context.Context, command, dir string) (Catalog, error) {
	return readPiRPC(ctx, command, dir)
}

// readPiRPC sets each model in turn to read its thinking levels.
func readPiRPC(ctx context.Context, command, dir string) (Catalog, error) {
	proc, err := start(command, dir, false)
	if err != nil {
		return Catalog{}, err
	}
	defer proc.stop()
	client := &piClient{proc: proc}
	var available struct {
		Models []piModel `json:"models"`
	}
	if err := client.call(ctx, map[string]any{"type": "get_available_models"}, &available); err != nil {
		return Catalog{}, err
	}
	var state struct {
		Model *piModel `json:"model"`
	}
	if err := client.call(ctx, map[string]any{"type": "get_state"}, &state); err != nil {
		return Catalog{}, err
	}
	var cat Catalog
	for _, model := range available.Models {
		var set piModel
		if err := client.call(ctx, map[string]any{"type": "set_model", "provider": model.Provider, "modelId": model.ID}, &set); err != nil {
			return Catalog{}, err
		}
		var thinking struct {
			Levels []string `json:"levels"`
		}
		if err := client.call(ctx, map[string]any{"type": "get_available_thinking_levels"}, &thinking); err != nil {
			return Catalog{}, err
		}
		cat.Models = append(cat.Models, Model{
			// pi's and omp's --model take provider/id as one pattern.
			ID:      model.Provider + "/" + model.ID,
			Label:   model.Name,
			Efforts: thinking.Levels,
			Default: state.Model != nil && state.Model.Provider == model.Provider && state.Model.ID == model.ID,
		})
	}
	return cat, nil
}

func checkPiVersion(ctx context.Context, command, dir string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return errors.New("empty catalog command")
	}
	cmd := exec.CommandContext(ctx, fields[0], "--version")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("pi --version: %w", err)
	}
	version := strings.TrimSpace(string(out))
	if !update.VersionWithin(version, piKeepsDefaults, "") {
		return fmt.Errorf("pi %s saves each model it lists as your default, the list needs pi %s or later", version, piKeepsDefaults)
	}
	return nil
}
