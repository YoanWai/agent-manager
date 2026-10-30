package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/YoanWai/agent-manager/internal/store"
)

const (
	usageExport        = "export [--settings] [--presets] [--groups] [--connections] [file|-]"
	usageImport        = "import [--settings] [--presets] [--groups] [--connections] [file|-]"
	configurationScope = "Configuration includes settings, keybindings, the editor, presets, groups and SSH connections. It excludes sessions, tasks, inbox messages, reviews, worktrees, agent history, environment profiles and SSH keys."
)

func transferSection(specs settingSpecs) section {
	return section{title: "Configuration transfer", commands: []command{
		{name: "export", usage: usageExport, about: "export configuration as JSON. All sections by default. May contain secrets", run: func(args []string, _ func() string, dir string) error {
			return runExport(os.Stdout, os.Stderr, specs, args, dir)
		}},
		{name: "import", usage: usageImport, about: "merge JSON configuration by matching keys and names. Unrelated entries remain", run: func(args []string, _ func() string, dir string) error {
			return runImport(os.Stdout, os.Stdin, specs, args, dir)
		}},
	}}
}

func transferArgs(out io.Writer, usage string, args []string) (store.ConfigurationSections, string, error) {
	set := newFlagSet(usage)
	sections := store.ConfigurationSections{}
	set.BoolVar(&sections.Settings, "settings", false, "include settings, keybindings and editor")
	set.BoolVar(&sections.Presets, "presets", false, "include session presets")
	set.BoolVar(&sections.Groups, "groups", false, "include groups in their saved order")
	set.BoolVar(&sections.Connections, "connections", false, "include SSH connections")
	operands, err := parseCommand(out, set, args, 0, 1)
	if err != nil {
		if errors.Is(err, ErrUsageShown) {
			fmt.Fprintln(out, configurationScope)
		}
		return sections, "", err
	}
	if sections == (store.ConfigurationSections{}) {
		sections = store.ConfigurationSections{Settings: true, Presets: true, Groups: true, Connections: true}
	}
	path := "-"
	if len(operands) == 1 {
		path = operands[0]
	}
	return sections, path, nil
}

func runExport(out, warnings io.Writer, specs settingSpecs, args []string, dir string) error {
	sections, path, err := transferArgs(out, usageExport, args)
	if err != nil {
		return err
	}
	all, err := specs()
	if err != nil {
		return err
	}
	st, err := openSettingsStore(dir)
	if err != nil {
		return err
	}
	defer st.Close()
	data, err := st.ExportConfiguration(sections, all)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	fmt.Fprintln(warnings, configurationScope)
	fmt.Fprintln(warnings, "Warning. Exported values are not redacted and may contain tokens, URLs or other secrets. Keep the file private.")
	if path == "-" {
		_, err = out.Write(encoded)
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(encoded)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		os.Remove(path)
		return err
	}
	_, err = fmt.Fprintln(out, "Exported configuration to "+path)
	return err
}

func runImport(out io.Writer, input io.Reader, specs settingSpecs, args []string, dir string) error {
	sections, path, err := transferArgs(out, usageImport, args)
	if err != nil {
		return err
	}
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		input = file
	}
	var data store.Configuration
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return fmt.Errorf("read configuration: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("configuration must contain exactly one JSON object")
	}
	if !sections.Settings {
		data.Settings = nil
	}
	if !sections.Presets {
		data.Presets = nil
	}
	if !sections.Groups {
		data.Groups = nil
	}
	if !sections.Connections {
		data.Connections = nil
	}
	all, err := specs()
	if err != nil {
		return err
	}
	st, err := openSettingsStore(dir)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.ImportConfiguration(data, all); err != nil {
		return err
	}
	fmt.Fprintf(out, "Imported %d settings, %d presets, %d groups and %d SSH connections. Matching keys and names were updated. Other entries were kept.\n", len(data.Settings), len(data.Presets), len(data.Groups), len(data.Connections))
	fmt.Fprintln(out, configurationScope)
	_, err = fmt.Fprintln(out, "Restart running managers to load the imported configuration. Review paths and commands before using them on this machine.")
	return err
}
