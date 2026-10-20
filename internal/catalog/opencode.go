package catalog

import (
	"context"
	"fmt"
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
	proc, err := start(command, dir, true)
	if err != nil {
		return Catalog{}, err
	}
	defer proc.stop()
	var cat Catalog
	var said []string
	for {
		select {
		case <-ctx.Done():
			return Catalog{}, ctx.Err()
		case line, ok := <-proc.lines:
			if !ok {
				if proc.waitErr != nil {
					return Catalog{}, fmt.Errorf("%w: %s", proc.waitErr, strings.Join(said, " "))
				}
				return cat, nil
			}
			text := strings.TrimSpace(string(line))
			if opencodeModel.MatchString(text) {
				cat.Models = append(cat.Models, Model{ID: text})
			} else if text != "" {
				said = append(said, text)
			}
		}
	}
}
