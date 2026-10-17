package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
)

const (
	usagePromptsList = "prompts list [--json]"
	usagePromptsAdd  = "prompts add <name> <text> [--json]"
	usagePromptsRm   = "prompts rm <name> [--json]"
)

func promptsSection() section {
	verbs := []command{
		{name: "list", usage: usagePromptsList, about: "list saved prompts by name", run: promptsCommand(runPromptsList)},
		{name: "add", usage: usagePromptsAdd, about: "save a named prompt, replacing one that already has that name", run: promptsCommand(runPromptsAdd)},
		{name: "rm", usage: usagePromptsRm, about: "delete a saved prompt", run: promptsCommand(runPromptsRm)},
	}
	return groupSection("Saved prompts", "prompts", "named instruction text for a new session or a message", verbs)
}

func promptsCommand(run func(io.Writer, []string, string) error) Command {
	return func(args []string, _ func() string, configDir string) error {
		return run(os.Stdout, args, configDir)
	}
}

type savedPromptRecord struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

func runPromptsList(out io.Writer, args []string, configDir string) error {
	set := newFlagSet(usagePromptsList)
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	st, err := openPromptStore(configDir)
	if err != nil {
		return err
	}
	defer st.Close()
	rows, err := st.SessionPresets()
	if err != nil {
		return err
	}
	listed := make([]savedPromptRecord, 0, len(rows))
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		listed = append(listed, savedPromptRecord{Name: row.Name, Text: row.Instructions})
		names = append(names, row.Name)
	}
	return emit(out, *asJSON, listed, strings.Join(names, "\n"))
}

func runPromptsAdd(out io.Writer, args []string, configDir string) error {
	set := newFlagSet(usagePromptsAdd)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 2, 2)
	if err != nil {
		return err
	}
	st, err := openPromptStore(configDir)
	if err != nil {
		return err
	}
	defer st.Close()
	name := strings.TrimSpace(operands[0])
	previous := ""
	if _, found, err := st.SessionPreset(name); err != nil {
		return err
	} else if found {
		previous = name
	}
	preset := store.SessionPreset{Name: name, Instructions: operands[1]}
	if err := st.SaveSessionPreset(previous, preset); err != nil {
		return err
	}
	return emit(out, *asJSON, savedPromptRecord{Name: name, Text: operands[1]}, "saved "+name)
}

func runPromptsRm(out io.Writer, args []string, configDir string) error {
	set := newFlagSet(usagePromptsRm)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	st, err := openPromptStore(configDir)
	if err != nil {
		return err
	}
	defer st.Close()
	deleted, err := st.DeleteSessionPreset(operands[0])
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("saved prompt %q does not exist", operands[0])
	}
	return emit(out, *asJSON, savedPromptRecord{Name: operands[0]}, "removed "+operands[0])
}

func openPromptStore(configDir string) (*store.Store, error) {
	return store.Open(filepath.Join(configDir, "state.db"))
}
