package cli

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
)

const (
	usageEnvList  = "env list [--json]"
	usageEnvShow  = "env show <name> [--json]"
	usageEnvSet   = "env set <name> <KEY=VALUE>... [--json]"
	usageEnvUnset = "env unset <name> <KEY>... [--json]"
	usageEnvRm    = "env rm <name> [--json]"
)

func envSection() section {
	verbs := []command{
		{name: "list", usage: usageEnvList, about: "list environment profiles with the variables each sets, values left out", run: envCommand(runEnvList)},
		{name: "show", usage: usageEnvShow, about: "print a profile's variables with their values", run: envCommand(runEnvShow)},
		{name: "set", usage: usageEnvSet, about: "set variables on a profile, creating it when it is new; the next launch of every session under it picks them up", run: envCommand(runEnvSet)},
		{name: "unset", usage: usageEnvUnset, about: "remove variables from a profile", run: envCommand(runEnvUnset)},
		{name: "rm", usage: usageEnvRm, about: "delete a profile; sessions still naming it refuse to launch until it is set again", run: envCommand(runEnvRm)},
	}
	return groupSection("Environment profiles", "env", "named environment variables a session launches under, picked per session or per group", verbs)
}

func envCommand(run func(io.Writer, []string, string) error) Command {
	return func(args []string, _ func() string, configDir string) error {
		return run(os.Stdout, args, configDir)
	}
}

type envProfileRecord struct {
	Name string            `json:"name"`
	Keys []string          `json:"keys"`
	Vars map[string]string `json:"vars,omitempty"`
}

func envRecord(profile store.EnvProfile, withValues bool) envProfileRecord {
	record := envProfileRecord{Name: profile.Name, Keys: envKeys(profile.Vars)}
	if withValues {
		record.Vars = profile.Vars
	}
	return record
}

func envKeys(vars map[string]string) []string {
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func runEnvList(out io.Writer, args []string, configDir string) error {
	set := newFlagSet(usageEnvList)
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	st, err := openSettingsStore(configDir)
	if err != nil {
		return err
	}
	defer st.Close()
	profiles, err := st.EnvProfiles()
	if err != nil {
		return err
	}
	listed := make([]envProfileRecord, 0, len(profiles))
	lines := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		record := envRecord(profile, false)
		listed = append(listed, record)
		lines = append(lines, strings.TrimSpace(record.Name+"  "+strings.Join(record.Keys, " ")))
	}
	return emit(out, *asJSON, listed, strings.Join(lines, "\n"))
}

func runEnvShow(out io.Writer, args []string, configDir string) error {
	set := newFlagSet(usageEnvShow)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	st, err := openSettingsStore(configDir)
	if err != nil {
		return err
	}
	defer st.Close()
	profile, found, err := st.EnvProfile(operands[0])
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("environment %q does not exist", operands[0])
	}
	return emit(out, *asJSON, envRecord(profile, true), envLines(profile.Vars))
}

func envLines(vars map[string]string) string {
	keys := envKeys(vars)
	lines := make([]string, len(keys))
	for i, key := range keys {
		lines[i] = key + "=" + vars[key]
	}
	return strings.Join(lines, "\n")
}

func runEnvSet(out io.Writer, args []string, configDir string) error {
	set := newFlagSet(usageEnvSet)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 2, anyNumber)
	if err != nil {
		return err
	}
	vars := make(map[string]string, len(operands)-1)
	for _, pair := range operands[1:] {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			return fmt.Errorf("%q is not KEY=VALUE", pair)
		}
		vars[key] = value
	}
	st, err := openSettingsStore(configDir)
	if err != nil {
		return err
	}
	defer st.Close()
	profile, err := st.SetEnvVars(operands[0], vars)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, envRecord(profile, false), "set "+strings.Join(envKeys(vars), " ")+" on "+profile.Name)
}

func runEnvUnset(out io.Writer, args []string, configDir string) error {
	set := newFlagSet(usageEnvUnset)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 2, anyNumber)
	if err != nil {
		return err
	}
	st, err := openSettingsStore(configDir)
	if err != nil {
		return err
	}
	defer st.Close()
	profile, err := st.UnsetEnvVars(operands[0], operands[1:])
	if err != nil {
		return err
	}
	return emit(out, *asJSON, envRecord(profile, false), "unset "+strings.Join(operands[1:], " ")+" on "+profile.Name)
}

func runEnvRm(out io.Writer, args []string, configDir string) error {
	set := newFlagSet(usageEnvRm)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	st, err := openSettingsStore(configDir)
	if err != nil {
		return err
	}
	defer st.Close()
	deleted, err := st.DeleteEnvProfile(operands[0])
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("environment %q does not exist", operands[0])
	}
	return emit(out, *asJSON, envProfileRecord{Name: operands[0], Keys: []string{}}, "removed "+operands[0])
}
