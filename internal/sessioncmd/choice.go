package sessioncmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/YoanWai/agent-manager/internal/catalog"
	"github.com/YoanWai/agent-manager/internal/config"
)

// loadCatalog stops asking on ctrl+c, so the CLI never outlives the command.
func loadCatalog(configDir, toolName string, tool config.Tool) (catalog.Catalog, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	return catalog.Load(ctx, configDir, toolName, tool)
}

// choose refuses what the CLI does not list, as the form does.
func (s *Sessions) choose(words Vocabulary, toolName string, tool config.Tool, opts CreateSessionOptions) (config.Choice, error) {
	model, effort, profile := strings.TrimSpace(opts.Model), strings.TrimSpace(opts.Effort), strings.TrimSpace(opts.Profile)
	if model == "" && effort == "" && profile == "" {
		return config.Choice{}, nil
	}
	if tool.Catalog == "" {
		return config.Choice{}, fmt.Errorf("%s does not report its models, so it starts on its own; leave out %s, %s and %s", toolName, words.SpawnModel, words.SpawnEffort, words.SpawnProfile)
	}
	if effort != "" && tool.EffortArgs == "" {
		return config.Choice{}, fmt.Errorf("%s takes no reasoning effort at launch; leave out %s", toolName, words.SpawnEffort)
	}
	if profile != "" && tool.ProfileArgs == "" {
		return config.Choice{}, fmt.Errorf("%s has no profiles to launch under; leave out %s", toolName, words.SpawnProfile)
	}
	cat, err := s.loadCatalog(s.configDir, toolName, tool)
	if err != nil {
		return config.Choice{}, fmt.Errorf("cannot read what %s offers to check the choice: %w", toolName, err)
	}
	choice := config.Choice{Effort: effort, Profile: profile}
	if profile != "" && !slices.ContainsFunc(cat.Profiles, func(p catalog.Profile) bool { return p.Name == profile }) {
		var names []string
		for _, p := range cat.Profiles {
			names = append(names, p.Name)
		}
		return config.Choice{}, fmt.Errorf("profile %q is not one %s has; it has %s", profile, toolName, strings.Join(names, ", "))
	}
	models := cat.ModelsFor(profile)
	picked, known := catalog.Default(models)
	if model != "" {
		matches := catalog.Match(models, model)
		switch len(matches) {
		case 0:
			return config.Choice{}, fmt.Errorf("model %q is not one %s lists; it lists %s", model, toolName, listed(models))
		case 1:
			picked, known = matches[0], true
		default:
			return config.Choice{}, fmt.Errorf("model %q is offered by more than one provider; pass one of %s", model, listed(matches))
		}
		choice.Model, choice.Provider = picked.ID, picked.Provider
	}
	if effort == "" || (known && picked.EffortTyped) {
		return choice, nil
	}
	if !known {
		return config.Choice{}, fmt.Errorf("%s does not say which model it starts on; pass %s with the effort", toolName, words.SpawnModel)
	}
	if !slices.Contains(picked.Efforts, effort) {
		if len(picked.Efforts) == 0 {
			return config.Choice{}, fmt.Errorf("%s takes no reasoning effort; leave out %s", picked.Key(), words.SpawnEffort)
		}
		return config.Choice{}, fmt.Errorf("effort %q is not one %s takes; it takes %s", effort, picked.Key(), strings.Join(picked.Efforts, ", "))
	}
	return choice, nil
}

// listed caps the names, since some CLIs list hundreds.
func listed(models []catalog.Model) string {
	const most = 20
	var keys []string
	for i, model := range models {
		if i == most {
			return strings.Join(keys, ", ") + fmt.Sprintf(" and %d more", len(models)-most)
		}
		keys = append(keys, model.Key())
	}
	return strings.Join(keys, ", ")
}
