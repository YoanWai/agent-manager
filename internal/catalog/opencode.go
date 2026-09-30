package catalog

import (
	"context"
	"net/http"
	"regexp"
	"sort"
)

var opencodeListening = regexp.MustCompile(`listening on (http://\S+)`)

// readOpencode starts opencode's headless server behind a one-use password
// and reads every provider's models from it. Its ACP server lists them too,
// but only inside a session, which opencode then keeps in its history.
func readOpencode(ctx context.Context, command, dir string) (Catalog, error) {
	password := secret()
	proc, err := start(command, dir, true, "OPENCODE_SERVER_PASSWORD="+password)
	if err != nil {
		return Catalog{}, err
	}
	defer proc.stop()
	address, err := serverAddress(ctx, proc, opencodeListening)
	if err != nil {
		return Catalog{}, err
	}
	auth := func(request *http.Request) { request.SetBasicAuth("opencode", password) }
	var providers struct {
		Providers []struct {
			ID     string `json:"id"`
			Models map[string]struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := getJSON(ctx, address+"/config/providers", auth, &providers); err != nil {
		return Catalog{}, err
	}
	var config struct {
		Model string `json:"model"`
	}
	if err := getJSON(ctx, address+"/config", auth, &config); err != nil {
		return Catalog{}, err
	}
	var cat Catalog
	for _, provider := range providers.Providers {
		for _, model := range provider.Models {
			id := provider.ID + "/" + model.ID
			cat.Models = append(cat.Models, Model{ID: id, Label: model.Name, Default: id == config.Model})
		}
	}
	sort.Slice(cat.Models, func(i, j int) bool { return cat.Models[i].ID < cat.Models[j].ID })
	return cat, nil
}
