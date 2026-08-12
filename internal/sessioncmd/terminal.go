package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

type Terminal struct {
	ID        string `json:"id" jsonschema:"managed terminal session id"`
	Name      string `json:"name" jsonschema:"terminal name shown in Agent Manager"`
	Group     string `json:"group" jsonschema:"group path holding the terminal; empty is the root"`
	Directory string `json:"directory" jsonschema:"terminal's current working directory, or its launch directory when stopped"`
	Status    string `json:"status" jsonschema:"stored Agent Manager status"`
	Running   bool   `json:"running" jsonschema:"whether the terminal currently has a live tmux pane"`
}

type TerminalScreen struct {
	Terminal Terminal `json:"terminal"`
	Output   string   `json:"output" jsonschema:"plain text currently visible in the terminal pane"`
}

type CreateTerminalOptions struct {
	// Nil inherits the calling session's group; a pointer to an empty string
	// deliberately targets the root group.
	Group     *string
	Directory string
}

type Terminals struct {
	configDir string
	newDriver func() (*tmux.Driver, error)
}

func NewTerminals(configDir string) *Terminals {
	return newTerminals(configDir, tmux.New)
}

func newTerminals(configDir string, newDriver func() (*tmux.Driver, error)) *Terminals {
	return &Terminals{configDir: configDir, newDriver: newDriver}
}

type terminalRuntime struct {
	cfg    config.Config
	store  *store.Store
	driver *tmux.Driver
}

func (t *Terminals) open() (*terminalRuntime, error) {
	cfg, err := config.LoadDir(t.configDir)
	if err != nil {
		return nil, err
	}
	driver, err := t.newDriver()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(filepath.Join(t.configDir, "state.db"))
	if err != nil {
		return nil, err
	}
	return &terminalRuntime{cfg: cfg, store: st, driver: driver}, nil
}

func (r *terminalRuntime) caller(sessionID string) (store.Session, error) {
	if err := validSession(sessionID); err != nil {
		return store.Session{}, err
	}
	sess, err := r.store.Get(sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, fmt.Errorf("calling session %s no longer exists", sessionID)
	}
	return sess, err
}

func (r *terminalRuntime) terminal(id string) (store.Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Session{}, errors.New("terminal_id is empty; call list_terminals to get one")
	}
	sess, err := r.store.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, fmt.Errorf("terminal %s does not exist; call list_terminals for current ids", id)
	}
	if err != nil {
		return store.Session{}, err
	}
	if !r.cfg.Tools[sess.Tool].Shell {
		return store.Session{}, fmt.Errorf("session %s is an agent, not a terminal", id)
	}
	if sess.Archived {
		return store.Session{}, fmt.Errorf("terminal %s is archived; restore it in Agent Manager first", id)
	}
	return sess, nil
}

func (r *terminalRuntime) info(sess store.Session, running bool) Terminal {
	dir := sess.Cwd
	if running {
		if current, err := r.driver.PaneCurrentPath(sess.ID); err == nil {
			dir = current
		}
	}
	return Terminal{
		ID:        sess.ID,
		Name:      sess.Name,
		Group:     sess.Group,
		Directory: dir,
		Status:    sess.Status,
		Running:   running,
	}
}

func (t *Terminals) List(sessionID string) ([]Terminal, error) {
	runtime, err := t.open()
	if err != nil {
		return nil, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return nil, err
	}
	sessions, err := runtime.store.ListSessions(false)
	if err != nil {
		return nil, err
	}
	panes, err := runtime.driver.Panes()
	if err != nil {
		return nil, err
	}
	terminals := make([]Terminal, 0)
	for _, sess := range sessions {
		if !runtime.cfg.Tools[sess.Tool].Shell {
			continue
		}
		_, running := panes[sess.ID]
		terminals = append(terminals, runtime.info(sess, running))
	}
	return terminals, nil
}

func (t *Terminals) Create(sessionID string, opts CreateTerminalOptions) (Terminal, error) {
	runtime, err := t.open()
	if err != nil {
		return Terminal{}, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return Terminal{}, err
	}
	toolName, tool, ok := runtime.cfg.ShellTool()
	if !ok {
		return Terminal{}, errors.New("no shell configured; add a tool block with shell = true to config.toml")
	}
	group, dir, err := runtime.createTarget(caller, opts)
	if err != nil {
		return Terminal{}, err
	}
	id := uuid.NewString()[:8]
	sess := store.Session{
		ID:     id,
		Name:   toolName + "-" + id[:4],
		Tool:   toolName,
		Cwd:    dir,
		Group:  group,
		Status: status.Starting,
	}
	if err := runtime.driver.Create(sess.ID, sess.Cwd, tool.Command, nil, 0, 0); err != nil {
		return Terminal{}, err
	}
	if err := runtime.store.CreateSession(sess); err != nil {
		_ = runtime.driver.Kill(sess.ID)
		return Terminal{}, err
	}
	_ = runtime.driver.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name))
	return runtime.info(sess, true), nil
}

func (r *terminalRuntime) createTarget(caller store.Session, opts CreateTerminalOptions) (string, string, error) {
	group := caller.Group
	groups, err := r.store.Groups()
	if err != nil {
		return "", "", err
	}
	byName := make(map[string]store.Group, len(groups))
	archived := make(map[string]bool, len(groups))
	for _, candidate := range groups {
		byName[candidate.Name] = candidate
		archived[candidate.Name] = candidate.Archived
	}
	if opts.Group != nil {
		group = strings.TrimSpace(*opts.Group)
		if group != "" {
			if _, ok := byName[group]; !ok {
				return "", "", fmt.Errorf("group %q does not exist", group)
			}
		}
	}
	if group != "" && store.EffectivelyArchived(archived, group) {
		return "", "", fmt.Errorf("group %q is archived; restore it in Agent Manager first", group)
	}
	if strings.TrimSpace(opts.Directory) != "" {
		dir, err := resolveTerminalDirectory(opts.Directory)
		return group, dir, err
	}
	if opts.Group != nil {
		for current := group; current != ""; current = parentGroup(current) {
			if candidate := byName[current].Path; candidate != "" {
				if dir, err := resolveTerminalDirectory(candidate); err == nil {
					return group, dir, nil
				}
			}
		}
	}
	dir := caller.Cwd
	if current, err := r.driver.PaneCurrentPath(caller.ID); err == nil {
		dir = current
	}
	resolved, err := resolveTerminalDirectory(dir)
	if err != nil {
		return "", "", fmt.Errorf("no usable directory for terminal: %w", err)
	}
	return group, resolved, nil
}

func resolveTerminalDirectory(raw string) (string, error) {
	dir := strings.TrimSpace(raw)
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("directory %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return abs, nil
}

func parentGroup(group string) string {
	if index := strings.LastIndex(group, "/"); index >= 0 {
		return group[:index]
	}
	return ""
}

func sessionLabel(group, name string) string {
	if group == "" {
		return name
	}
	return group + " · " + name
}

func (t *Terminals) Send(sessionID, terminalID, command string, keys []string) error {
	hasCommand := strings.TrimSpace(command) != ""
	hasKeys := len(keys) > 0
	if hasCommand == hasKeys {
		return errors.New("provide exactly one of command or keys")
	}
	for _, key := range keys {
		if key == "" {
			return errors.New("keys cannot contain an empty value")
		}
	}
	runtime, err := t.open()
	if err != nil {
		return err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return err
	}
	terminal, err := runtime.terminal(terminalID)
	if err != nil {
		return err
	}
	if !runtime.driver.Exists(terminal.ID) {
		return fmt.Errorf("terminal %s is not running; revive it in Agent Manager first", terminal.ID)
	}
	if hasCommand {
		return runtime.driver.SendText(terminal.ID, command)
	}
	return runtime.driver.SendKeys(terminal.ID, keys...)
}

func (t *Terminals) Read(sessionID, terminalID string) (TerminalScreen, error) {
	runtime, err := t.open()
	if err != nil {
		return TerminalScreen{}, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return TerminalScreen{}, err
	}
	terminal, err := runtime.terminal(terminalID)
	if err != nil {
		return TerminalScreen{}, err
	}
	if !runtime.driver.Exists(terminal.ID) {
		return TerminalScreen{}, fmt.Errorf("terminal %s is not running; revive it in Agent Manager first", terminal.ID)
	}
	output, err := runtime.driver.CapturePane(terminal.ID)
	if err != nil {
		return TerminalScreen{}, err
	}
	return TerminalScreen{
		Terminal: runtime.info(terminal, true),
		Output:   strings.TrimRight(ansi.Strip(output), "\r\n"),
	}, nil
}
