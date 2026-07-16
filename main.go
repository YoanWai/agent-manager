package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "agent-manager:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	driver, err := tmux.New()
	if err != nil {
		return err
	}

	engine, err := status.NewEngine(cfg)
	if err != nil {
		return err
	}

	dir, err := config.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	model := ui.New(cfg, st, driver, engine)
	program := tea.NewProgram(model, tea.WithAltScreen())
	model.StartPoller(program.Send)
	_, err = program.Run()
	return err
}
