package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/YoanWai/agent-manager/internal/store"
)

const (
	usageSettingsList = "settings list [--json]"
	usageSettingsGet  = "settings get <key> [--json]"
	usageSettingsSet  = "settings set <key> <value> [--json]"
)

// settingSpecs is read when a settings verb runs, not when the command
// table is built for every other subcommand.
type settingSpecs func() ([]store.SettingSpec, error)

func settingsSection(specs settingSpecs) section {
	verbs := []command{
		{name: "list", usage: usageSettingsList, about: "every setting with its value, its Settings row and the values it takes", run: settingsCommand(specs, runSettingsList)},
		{name: "get", usage: usageSettingsGet, about: "print one setting's value, or its default when none is stored", run: settingsCommand(specs, runSettingsGet)},
		{name: "set", usage: usageSettingsSet, about: "change a setting from a shell; the value is checked against the ones its Settings row steps through", run: settingsCommand(specs, runSettingsSet)},
	}
	return groupSection("Settings", "settings", "read or change the settings the manager stores, without opening it", verbs)
}

func settingsCommand(specs settingSpecs, run func(io.Writer, settingSpecs, []string, string) error) Command {
	return func(args []string, _ func() string, configDir string) error {
		return run(os.Stdout, specs, args, configDir)
	}
}

type settingRecord struct {
	Key     string   `json:"key"`
	Value   string   `json:"value"`
	Default string   `json:"default,omitempty"`
	Values  []string `json:"values,omitempty"`
	List    bool     `json:"list,omitempty"`
	Row     string   `json:"row,omitempty"`
}

func runSettingsList(out io.Writer, specs settingSpecs, args []string, configDir string) error {
	set := newFlagSet(usageSettingsList)
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	all, err := specs()
	if err != nil {
		return err
	}
	st, err := openSettingsStore(configDir)
	if err != nil {
		return err
	}
	defer st.Close()
	stored, err := st.Settings()
	if err != nil {
		return err
	}
	records := make([]settingRecord, 0, len(all))
	for _, spec := range all {
		records = append(records, settingRecord{
			Key: spec.Key, Value: effective(spec, stored[spec.Key]), Default: spec.Default,
			Values: spec.Values, List: spec.List, Row: spec.Row,
		})
	}
	if *asJSON {
		return writeJSON(out, records)
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, record := range records {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", record.Key, record.Value, record.Row, record.takes())
	}
	return table.Flush()
}

func runSettingsGet(out io.Writer, specs settingSpecs, args []string, configDir string) error {
	set := newFlagSet(usageSettingsGet)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	spec, err := lookupSetting(specs, operands[0])
	if err != nil {
		return err
	}
	st, err := openSettingsStore(configDir)
	if err != nil {
		return err
	}
	defer st.Close()
	stored, err := st.Setting(spec.Key)
	if err != nil {
		return err
	}
	value := effective(spec, stored)
	return emit(out, *asJSON, settingRecord{Key: spec.Key, Value: value}, value)
}

func runSettingsSet(out io.Writer, specs settingSpecs, args []string, configDir string) error {
	set := newFlagSet(usageSettingsSet)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 2, 2)
	if err != nil {
		return err
	}
	spec, err := lookupSetting(specs, operands[0])
	if err != nil {
		return err
	}
	value := operands[1]
	if err := spec.Check(value); err != nil {
		return err
	}
	st, err := openSettingsStore(configDir)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.SetSetting(spec.Key, value); err != nil {
		return err
	}
	human := spec.Key + " set to " + value
	if value == "" {
		human = spec.Key + " cleared"
	}
	return emit(out, *asJSON, settingRecord{Key: spec.Key, Value: value}, human)
}

func lookupSetting(specs settingSpecs, key string) (store.SettingSpec, error) {
	all, err := specs()
	if err != nil {
		return store.SettingSpec{}, err
	}
	for _, spec := range all {
		if spec.Key == key {
			return spec, nil
		}
	}
	return store.SettingSpec{}, fmt.Errorf("no setting is called %q; agent-manager settings list names them", key)
}

// openSettingsStore makes the directory too, so a setup script can set a
// value before the manager has ever run.
func openSettingsStore(configDir string) (*store.Store, error) {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, err
	}
	return store.Open(filepath.Join(configDir, "state.db"))
}

func effective(spec store.SettingSpec, stored string) string {
	if stored == "" {
		return spec.Default
	}
	return stored
}

func (r settingRecord) takes() string {
	switch {
	case len(r.Values) == 0:
		return "any text"
	case r.List:
		return "any of " + strings.Join(r.Values, ", ")
	default:
		return "one of " + strings.Join(r.Values, ", ")
	}
}
