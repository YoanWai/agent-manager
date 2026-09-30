package catalog

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var hermesReady = regexp.MustCompile(`HERMES_BACKEND_READY port=(\d+)`)

type hermesOptions struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Rows     []struct {
		Slug         string   `json:"slug"`
		Name         string   `json:"name"`
		Models       []string `json:"models"`
		Capabilities map[string]struct {
			Reasoning bool `json:"reasoning"`
		} `json:"capabilities"`
	} `json:"providers"`
}

// models lists every logged-in provider's models. Hermes publishes whether a
// model reasons but not the levels it takes, so those are typed.
func (o hermesOptions) models() []Model {
	var models []Model
	for _, row := range o.Rows {
		for _, id := range row.Models {
			models = append(models, Model{
				ID:          id,
				Provider:    row.Slug,
				Label:       row.Name,
				EffortTyped: row.Capabilities[id].Reasoning,
				Default:     row.Slug == o.Provider && id == o.Model,
			})
		}
	}
	return models
}

// readHermes starts hermes serve behind a one-use session token, lists its
// profiles, and reads the model options once for the active profile and
// once scoped to each named one.
func readHermes(ctx context.Context, command, dir string) (Catalog, error) {
	token := secret()
	proc, err := start(command, dir, true, "HERMES_DASHBOARD_SESSION_TOKEN="+token)
	if err != nil {
		return Catalog{}, err
	}
	defer proc.stop()
	port, err := serverAddress(ctx, proc, hermesReady)
	if err != nil {
		return Catalog{}, err
	}
	base := "http://127.0.0.1:" + port
	auth := func(request *http.Request) { request.Header.Set("X-Hermes-Session-Token", token) }
	var listed struct {
		Profiles []struct {
			Name     string `json:"name"`
			Model    string `json:"model"`
			Provider string `json:"provider"`
		} `json:"profiles"`
	}
	if err := getJSON(ctx, base+"/api/profiles", auth, &listed); err != nil {
		return Catalog{}, err
	}
	var active hermesOptions
	if err := getJSON(ctx, base+"/api/model/options", auth, &active); err != nil {
		return Catalog{}, err
	}
	cat := Catalog{Models: active.models()}
	for _, profile := range listed.Profiles {
		var scoped hermesOptions
		if err := getJSON(ctx, base+"/api/model/options?profile="+url.QueryEscape(profile.Name), auth, &scoped); err != nil {
			return Catalog{}, err
		}
		detail := strings.Join(nonEmpty(profile.Model, profile.Provider), " · ")
		cat.Profiles = append(cat.Profiles, Profile{Name: profile.Name, Detail: detail, Models: scoped.models()})
	}
	return cat, nil
}

func nonEmpty(values ...string) []string {
	var kept []string
	for _, value := range values {
		if value != "" {
			kept = append(kept, value)
		}
	}
	return kept
}
