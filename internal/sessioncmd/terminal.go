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

// TerminalInput is what one send put into a terminal. Which of the two
// kinds went in is decided here, where the dispatch happens, so neither
// front reads it back off its own arguments.
type TerminalInput struct {
	TerminalID string `json:"terminal_id"`
	Sent       string `json:"sent" jsonschema:"input kind sent: command or keys"`
}

type CreateTerminalOptions struct {
	// Nil inherits the calling session's group; a pointer to an empty string
	// deliberately targets the root group.
	Group     *string
	Directory string
}

type Terminals struct {
	commands
}

func NewTerminals(configDir string, words Vocabulary) *Terminals {
	return newTerminals(configDir, words, tmux.New)
}

func newTerminals(configDir string, words Vocabulary, newDriver func() (*tmux.Driver, error)) *Terminals {
	return &Terminals{commands: commands{configDir: configDir, words: words, newDriver: newDriver}}
}

// commands is the shared plumbing of every managed-pane command: the
// manager's config directory, the words the calling front speaks, and the
// tmux driver behind its socket.
type commands struct {
	configDir string
	words     Vocabulary
	newDriver func() (*tmux.Driver, error)
}

type runtime struct {
	cfg    config.Config
	words  Vocabulary
	store  *store.Store
	driver *tmux.Driver
}

func (c *commands) open() (*runtime, error) {
	cfg, err := config.LoadDir(c.configDir)
	if err != nil {
		return nil, err
	}
	driver, err := c.newDriver()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(filepath.Join(c.configDir, "state.db"))
	if err != nil {
		return nil, err
	}
	return &runtime{cfg: cfg, words: c.words, store: st, driver: driver}, nil
}

func (r *runtime) caller(sessionID string) (store.Session, error) {
	if err := validSession(sessionID); err != nil {
		return store.Session{}, err
	}
	sess, err := r.store.Get(sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, fmt.Errorf("calling session %s no longer exists", sessionID)
	}
	return sess, err
}

func (r *runtime) terminal(id string) (store.Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Session{}, fmt.Errorf("terminal_id is empty; call %s to get one", r.words.ListTerminals)
	}
	sess, err := r.store.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, fmt.Errorf("terminal %s does not exist; call %s for current ids", id, r.words.ListTerminals)
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

func (r *runtime) info(sess store.Session, running bool) Terminal {
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
	group, dir, err := runtime.createTarget(caller, opts.Group, opts.Directory)
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

// createTarget resolves the group and directory a new pane opens in.
// A nil requested group inherits the caller's; an explicit one must
// already exist. An explicit directory wins outright; a caller that named
// a group falls back to that group's nearest inherited default path; a
// caller that named none opens beside itself.
func (r *runtime) createTarget(caller store.Session, requestedGroup *string, directory string) (string, string, error) {
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
	if requestedGroup != nil {
		group = strings.TrimSpace(*requestedGroup)
		if group != "" {
			if _, ok := byName[group]; !ok {
				return "", "", fmt.Errorf("group %q does not exist; call %s for the existing ones or %s to add it", group, r.words.ListGroups, r.words.CreateGroup)
			}
		}
	}
	if group != "" && store.EffectivelyArchived(archived, group) {
		return "", "", fmt.Errorf("group %q is archived; restore it in Agent Manager first", group)
	}
	if strings.TrimSpace(directory) != "" {
		dir, err := resolveTerminalDirectory(directory)
		return group, dir, err
	}
	if requestedGroup != nil {
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
		if dir == "~" {
			dir = home
		} else {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
		}
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

func (t *Terminals) Send(sessionID, terminalID, command string, keys []string) (TerminalInput, error) {
	hasCommand := strings.TrimSpace(command) != ""
	hasKeys := len(keys) > 0
	if hasCommand == hasKeys {
		return TerminalInput{}, errors.New("provide exactly one of command or keys")
	}
	for _, key := range keys {
		if key == "" {
			return TerminalInput{}, errors.New("keys cannot contain an empty value")
		}
	}
	runtime, err := t.open()
	if err != nil {
		return TerminalInput{}, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return TerminalInput{}, err
	}
	terminal, err := runtime.terminal(terminalID)
	if err != nil {
		return TerminalInput{}, err
	}
	if !runtime.driver.Exists(terminal.ID) {
		return TerminalInput{}, fmt.Errorf("terminal %s is not running; revive it in Agent Manager first", terminal.ID)
	}
	if hasCommand {
		if err := runtime.driver.SendText(terminal.ID, command); err != nil {
			return TerminalInput{}, err
		}
		return TerminalInput{TerminalID: terminal.ID, Sent: "command"}, nil
	}
	if err := runtime.driver.SendKeys(terminal.ID, keys...); err != nil {
		return TerminalInput{}, err
	}
	return TerminalInput{TerminalID: terminal.ID, Sent: "keys"}, nil
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
