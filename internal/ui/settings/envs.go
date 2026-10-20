package settings

import "strings"

// Env is an environment profile as Settings lists it: its name and the
// variables it sets, never their values, which may be tokens.
type Env struct {
	Name string
	Keys []string
}

func envSummary(envs []Env) string {
	if len(envs) == 0 {
		return "none · agent-manager env set <name> KEY=VALUE defines one"
	}
	parts := make([]string, len(envs))
	for i, env := range envs {
		parts[i] = env.Name
		if len(env.Keys) > 0 {
			parts[i] += " (" + strings.Join(env.Keys, " ") + ")"
		}
	}
	return strings.Join(parts, " · ")
}
