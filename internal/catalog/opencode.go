package catalog

import (
	"context"
	"regexp"
	"strings"
)

// opencodeModel is one row of `opencode models`: provider/model, the form
// its launch takes. A model id can hold slashes of its own.
var opencodeModel = regexp.MustCompile(`^[^\s/]+/\S+$`)

// opencode's server answers the list before its plugins have settled on a
// cold start, and says so; the next ask lands after they have.
const opencodeSettling = "did not finish activating"

const opencodeAsks = 3

// `opencode models` names no default and no labels, so the picker shows ids.
func readOpencode(ctx context.Context, command, dir string) (Catalog, error) {
	for ask := 1; ; ask++ {
		cat, err := askOpencode(ctx, command, dir)
		if err == nil || ask == opencodeAsks || !strings.Contains(err.Error(), opencodeSettling) {
			return cat, err
		}
	}
}

func askOpencode(ctx context.Context, command, dir string) (Catalog, error) {
	ids, err := printedRows(ctx, command, dir, opencodeModel)
	if err != nil {
		return Catalog{}, err
	}
	var cat Catalog
	for _, id := range ids {
		cat.Models = append(cat.Models, Model{ID: id})
	}
	return cat, nil
}
