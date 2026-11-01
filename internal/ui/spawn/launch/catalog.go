package launch

import (
	"context"
	"errors"
	"time"

	"github.com/YoanWai/agent-manager/internal/catalog"
	"github.com/YoanWai/agent-manager/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// CatalogState is one CLI's answer: what it listed, why it failed, and when
// it was asked, so a running manager rereads a kept answer without asking on
// every frame.
type CatalogState struct {
	Cat       catalog.Catalog
	Err       error
	Loaded    bool
	Loading   bool
	CheckedAt time.Time
}

// A running manager rereads the kept answer to catch a CLI upgrade or a
// spawn's refresh, and retries a failure sooner, since a login may be all it
// lacked.
const (
	CatalogRecheck = 10 * time.Minute
	CatalogRetry   = 30 * time.Second
)

// CatalogMsg carries a CLI's answer. Stale is shown while a fresh answer is
// asked.
type CatalogMsg struct {
	Tool  string
	Cat   catalog.Catalog
	Err   error
	Stale bool
}

// EnsureCatalog reads the kept answer or asks the CLI off the update path,
// since an answer takes up to seconds. It reports whether a read is due.
func (s *State) EnsureCatalog(cfg config.Config, configDir, toolName string, now time.Time) (tea.Cmd, bool) {
	tool := cfg.CLIs[toolName]
	if tool.Catalog == "" {
		return nil, false
	}
	if s.Catalogs == nil {
		s.Catalogs = map[string]*CatalogState{}
	}
	state := s.Catalogs[toolName]
	if state == nil {
		state = &CatalogState{}
		s.Catalogs[toolName] = state
	}
	if state.Loading {
		return nil, false
	}
	// An old CLI changes when the user updates it, so the next open asks again.
	var outdated config.OutdatedToolError
	if !errors.As(state.Err, &outdated) {
		recheck := CatalogRecheck
		if state.Err != nil {
			recheck = CatalogRetry
		}
		if state.Loaded && now.Sub(state.CheckedAt) < recheck {
			return nil, false
		}
	}
	state.Loading = true
	return func() tea.Msg {
		if err := config.CheckVersion(tool); err != nil {
			return CatalogMsg{Tool: toolName, Err: err}
		}
		if cat, fresh, ok := catalog.Cached(configDir, toolName, tool); ok {
			return CatalogMsg{Tool: toolName, Cat: cat, Stale: !fresh}
		}
		return refreshCatalog(configDir, toolName, tool)
	}, true
}

func refreshCatalog(configDir, toolName string, tool config.CLI) tea.Msg {
	cat, err := catalog.Refresh(context.Background(), configDir, toolName, tool)
	return CatalogMsg{Tool: toolName, Cat: cat, Err: err}
}

// Apply keeps a CLI's answer, and asks again when the kept one was stale,
// returning the command that asks. Outdated reports the CLI version error
// the answer held, so the root can open its update dialog.
func (s *State) Apply(msg CatalogMsg, cfg config.Config, configDir string, now time.Time) (outdated *config.OutdatedToolError, cmd tea.Cmd) {
	state := s.Catalogs[msg.Tool]
	if state == nil {
		state = &CatalogState{}
		s.Catalogs[msg.Tool] = state
	}
	if msg.Err == nil {
		state.Cat = msg.Cat
	}
	state.Err = msg.Err
	state.Loaded = true
	state.Loading = msg.Stale
	state.CheckedAt = now
	var found config.OutdatedToolError
	if errors.As(msg.Err, &found) {
		outdated = &found
	}
	if !msg.Stale {
		return outdated, nil
	}
	tool := cfg.CLIs[msg.Tool]
	return outdated, func() tea.Msg { return refreshCatalog(configDir, msg.Tool, tool) }
}

// Answer hands a caller a copy of one CLI's answer, so only Apply changes
// what the CLI said.
func (s *State) Answer(toolName string) *Answer {
	state := s.Catalogs[toolName]
	if state == nil {
		return nil
	}
	return &Answer{Cat: state.Cat, Err: state.Err, Loaded: state.Loaded, Loading: state.Loading}
}

type Answer struct {
	Cat     catalog.Catalog
	Err     error
	Loaded  bool
	Loading bool
}
