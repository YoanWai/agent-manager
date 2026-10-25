package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// antigravityRow is one row of `agy models`: the id, a tab, the label.
var antigravityRow = regexp.MustCompile(`^\S+\t.+$`)

// agy lists each thinking level of a model as a row of its own, the level
// named in the label and suffixed to the id ("Gemini 3.8 Flash (High)",
// gemini-3.8-flash-high). The base id with --effort reaches the same model.
// The levels are the ones --effort takes and Google's model docs name.
var antigravityLevel = regexp.MustCompile(`^(.+) \((Low|Medium|High|Max)\)$`)

// readAntigravity folds the levels of a model into its efforts, so the
// picker offers one model and its levels apart, as for every other CLI.
func readAntigravity(ctx context.Context, command, dir string) (Catalog, error) {
	saved, err := antigravitySavedModel()
	if err != nil {
		return Catalog{}, err
	}
	rows, err := printedRows(ctx, command, dir, antigravityRow)
	if err != nil {
		return Catalog{}, err
	}
	var cat Catalog
	seen := map[string]int{}
	for _, row := range rows {
		id, label, _ := strings.Cut(row, "\t")
		base, baseLabel, level := antigravityModel(id, label)
		index, ok := seen[base]
		if !ok {
			index = len(cat.Models)
			seen[base] = index
			cat.Models = append(cat.Models, Model{ID: base, Label: baseLabel})
		}
		if level != "" {
			cat.Models[index].Efforts = append(cat.Models[index].Efforts, level)
		}
		if label == saved {
			cat.Models[index].Default = true
		}
	}
	return cat, nil
}

func antigravityModel(id, label string) (base, baseLabel, level string) {
	match := antigravityLevel.FindStringSubmatch(label)
	if match == nil {
		return id, label, ""
	}
	level = strings.ToLower(match[2])
	base, ok := strings.CutSuffix(id, "-"+level)
	if !ok {
		return id, label, ""
	}
	return base, match[1], level
}

// antigravitySavedModel is the label of the model agy starts on, which it
// keeps in its settings once one has been picked.
func antigravitySavedModel() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var settings struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return "", err
	}
	return settings.Model, nil
}
