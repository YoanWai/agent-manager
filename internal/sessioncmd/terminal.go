package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

type Terminal struct {
	ID         string `json:"id" jsonschema:"managed terminal session id"`
	Name       string `json:"name" jsonschema:"terminal name shown in Agent Manager"`
	Group      string `json:"group" jsonschema:"group path holding the terminal; empty is the root"`
	Directory  string `json:"directory" jsonschema:"terminal's current working directory, or its launch directory when stopped"`
	Status     string `json:"status" jsonschema:"stored Agent Manager status"`
	Running    bool   `json:"running" jsonschema:"whether the terminal currently has a live tmux pane"`
	ParentID   string `json:"parent_id" jsonschema:"id of the parent session when nested; empty when un-nested"`
	ParentName string `json:"parent_name" jsonschema:"name of the parent session when nested; empty when un-nested"`
	Archived   bool   `json:"archived,omitempty" jsonschema:"whether the terminal is archived out of the active list"`
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
	Nest      *bool
}

type Terminals struct {
	commands
}

func NewTerminals(configDir string, words Vocabulary) *Terminals {
	return newTerminals(configDir, words, tmux.New)
}

func NewTerminalsWithBackend(backend *Backend, words Vocabulary) *Terminals {
	if backend == nil {
		panic("session command backend is required")
	}
	return &Terminals{commands: commands{words: words, backend: backend}}
}

func newTerminals(configDir string, words Vocabulary, newDriver func() (*tmux.Driver, error)) *Terminals {
	return &Terminals{commands: commands{configDir: configDir, words: words, newDriver: newDriver, loadConfig: config.Default}}
}

// childKind is one kind of row that hangs under a session in its own pane,
// a terminal or a browser, as the tools that drive it name and find it.
type childKind struct {
	noun     string
	listWord func(Vocabulary) string
	toolName string
}

var terminalKind = childKind{
	noun:     "terminal",
	listWord: func(words Vocabulary) string { return words.ListTerminals },
	toolName: config.TerminalTool,
}

func (r *runtime) child(kind childKind, id string) (store.Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Session{}, fmt.Errorf("%s_id is empty; call %s to get one", kind.noun, kind.listWord(r.words))
	}
	sess, err := r.store.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, fmt.Errorf("%s %s does not exist; call %s for current ids", kind.noun, id, kind.listWord(r.words))
	}
	if err != nil {
		return store.Session{}, err
	}
	if sess.Tool != kind.toolName {
		return store.Session{}, fmt.Errorf("session %s is not a %s", id, kind.noun)
	}
	if sess.Archived {
		return store.Session{}, fmt.Errorf("%s %s is archived; restore it in Agent Manager first", kind.noun, id)
	}
	return sess, nil
}

func (r *runtime) nestedChild(kind childKind, sessionID, childID string) (store.Session, error) {
	if sessionID == "" {
		return r.unnestedChild(kind, childID)
	}
	caller, err := r.caller(sessionID)
	if err != nil {
		return store.Session{}, err
	}
	child, err := r.child(kind, childID)
	if err != nil {
		return store.Session{}, err
	}
	if child.ParentID != caller.ID {
		return store.Session{}, fmt.Errorf("%s %s is not nested under this session", kind.noun, child.ID)
	}
	return child, nil
}

// unnestedChild serves a caller with no session, such as a manager on
// another machine. It reaches only the rows such a caller opened: a nested
// one stays its session's, and one the user or a session opened stays out
// of reach of an agent that merely lost its session environment.
func (r *runtime) unnestedChild(kind childKind, childID string) (store.Session, error) {
	child, err := r.child(kind, childID)
	if err != nil {
		return store.Session{}, err
	}
	if child.ParentID != "" {
		return store.Session{}, fmt.Errorf("%s %s belongs to session %s; only it can drive it", kind.noun, child.ID, child.ParentID)
	}
	if !child.OpenedWithoutSession {
		return store.Session{}, fmt.Errorf("%s %s was opened in Agent Manager, not by a caller with no session; only Agent Manager and its sessions can drive it", kind.noun, child.ID)
	}
	return child, nil
}

func (r *runtime) info(sess store.Session, running bool) (Terminal, error) {
	parentName := ""
	if sess.ParentID != "" {
		// A parent row that is gone leaves the terminal orphaned, which the
		// list paints un-nested; anything else is a store failure.
		parent, err := r.store.Get(sess.ParentID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return Terminal{}, fmt.Errorf("parent %s of terminal %s: %w", sess.ParentID, sess.ID, err)
		default:
			parentName = parent.Name
		}
	}
	return r.terminalInfo(sess, running, parentName), nil
}

func (r *runtime) terminalInfo(sess store.Session, running bool, parentName string) Terminal {
	dir := sess.Cwd
	if running {
		if current, err := r.driver.PaneCurrentPath(sess.ID); err == nil {
			dir = current
		}
	}
	return Terminal{
		ID:         sess.ID,
		Name:       sess.Name,
		Group:      sess.Group,
		Directory:  dir,
		Status:     sess.Status,
		Running:    running,
		ParentID:   sess.ParentID,
		ParentName: parentName,
		Archived:   sess.Archived,
	}
}

func (t *Terminals) List(sessionID string) ([]Terminal, error) {
	terminals, _, err := t.listChildren(terminalKind, sessionID)
	return terminals, err
}

// listChildren lists every active row of kind, with the stored rows in the
// same order for a caller that reads fields the info leaves out.
func (c *commands) listChildren(kind childKind, sessionID string) ([]Terminal, []store.Session, error) {
	runtime, err := c.open()
	if err != nil {
		return nil, nil, err
	}
	defer runtime.Close()
	return runtime.listChildren(kind, sessionID)
}

func (r *runtime) listChildren(kind childKind, sessionID string) ([]Terminal, []store.Session, error) {
	if _, err := r.caller(sessionID); err != nil {
		return nil, nil, err
	}
	sessions, err := r.store.ListSessions(false)
	if err != nil {
		return nil, nil, err
	}
	panes, err := r.driver.Panes()
	if err != nil {
		return nil, nil, err
	}
	infos := make([]Terminal, 0)
	rows := make([]store.Session, 0)
	for _, sess := range sessions {
		if sess.Tool != kind.toolName {
			continue
		}
		_, running := panes[sess.ID]
		info, err := r.info(sess, running)
		if err != nil {
			return nil, nil, err
		}
		infos = append(infos, info)
		rows = append(rows, sess)
	}
	return infos, rows, nil
}

func (t *Terminals) Create(sessionID string, opts CreateTerminalOptions) (Terminal, error) {
	runtime, err := t.open()
	if err != nil {
		return Terminal{}, err
	}
	defer runtime.Close()
	caller, err := runtime.optionalCaller(sessionID)
	if err != nil {
		return Terminal{}, err
	}
	_, terminal := runtime.cfg.Terminal()
	launched, err := t.launchChild(runtime, caller, childLaunch{
		kind:      terminalKind,
		command:   terminal.Command,
		group:     opts.Group,
		directory: opts.Directory,
		nest:      opts.Nest,
	})
	if err != nil {
		return Terminal{}, err
	}
	return runtime.info(launched, true)
}

type childLaunch struct {
	kind       childKind
	command    string
	browserURL string
	group      *string
	directory  string
	nest       *bool
}

func (c *commands) launchChild(runtime *runtime, caller store.Session, request childLaunch) (store.Session, error) {
	nest := caller.ID != ""
	if request.nest != nil {
		nest = *request.nest
	}
	if nest && caller.ID == "" {
		return store.Session{}, fmt.Errorf("there is no calling session to nest the %s under; leave nest unset or false", request.kind.noun)
	}
	if nest && request.group != nil && strings.TrimSpace(*request.group) != caller.Group {
		return store.Session{}, errors.New("set nest false to place in another group")
	}
	group, dir, err := runtime.createTarget(caller, request.group, request.directory)
	if err != nil {
		return store.Session{}, err
	}
	// A terminal or browser caller hangs under a session itself, and nesting
	// is one level, so the new row joins it as a sibling instead.
	callerIsChild := runtime.cfg.IsTool(caller.Tool)
	parentID := ""
	if nest {
		parentID = caller.ID
		if callerIsChild {
			parentID = caller.ParentID
		}
	}
	name, err := runtime.shellName(request.kind.toolName, parentID)
	if err != nil {
		return store.Session{}, err
	}
	environment, err := runtime.environmentFor(caller, group, nil)
	if err != nil {
		return store.Session{}, err
	}
	sess := store.Session{
		ID:                   uuid.NewString()[:8],
		Name:                 name,
		Tool:                 request.kind.toolName,
		Cwd:                  dir,
		Group:                group,
		Status:               status.Starting,
		ParentID:             parentID,
		OpenedWithoutSession: caller.ID == "",
		Environment:          environment,
		BrowserURL:           request.browserURL,
	}
	lifecycle, err := c.lifecycle(runtime, nil)
	if err != nil {
		return store.Session{}, err
	}
	besideID := ""
	if nest && callerIsChild {
		besideID = caller.ID
	}
	launched, err := lifecycle.Launch(LaunchRequest{
		Session:         sess,
		Tool:            runtime.cfg.Tools[request.kind.toolName].Launch(),
		BaseCommand:     request.command,
		BesideSessionID: besideID,
	})
	if err != nil {
		return store.Session{}, err
	}
	return launched.Session, nil
}

func (t *Terminals) Close(sessionID, terminalID string) error {
	return t.closeChild(terminalKind, sessionID, terminalID, nil)
}

func (c *commands) closeChild(kind childKind, sessionID, childID string, beforeKill func(*runtime, store.Session)) error {
	runtime, err := c.open()
	if err != nil {
		return err
	}
	defer runtime.Close()
	sess, err := runtime.nestedChild(kind, sessionID, childID)
	if err != nil {
		return err
	}
	if beforeKill != nil {
		beforeKill(runtime, sess)
	}
	return runtime.store.DeleteChild(sess.ID, sess.ParentID, func() error {
		return runtime.driver.Kill(sess.ID)
	})
}

func (r *runtime) shellName(toolName, parentID string) (string, error) {
	sessions, err := r.store.ListSessions(true)
	if err != nil {
		return "", err
	}
	return ShellName(toolName, parentID, uuid.NewString()[:4], sessions), nil
}

// ShellName names a terminal after the session it hangs under, so a row
// says which session opened it rather than four random digits. A terminal
// with no session over it falls back to those digits, and one joining
// terminals already named for that session counts up. Both the list's t
// and the terminal tools name through here, so a shell reads the same
// whichever opened it.
func ShellName(toolName, parentID, fallbackSuffix string, sessions []store.Session) string {
	parentName := ""
	taken := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		taken[sess.Name] = true
		if sess.ID == parentID {
			parentName = sess.Name
		}
	}
	if parentName == "" {
		return toolName + "-" + fallbackSuffix
	}
	base := toolName + "-" + parentName
	name := base
	for n := 2; taken[name]; n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	return name
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
	defer runtime.Close()
	terminal, err := runtime.nestedChild(terminalKind, sessionID, terminalID)
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
	return t.read(sessionID, terminalID, false)
}

// ReadStyled is Read with the screen's escape sequences kept.
func (t *Terminals) ReadStyled(sessionID, terminalID string) (TerminalScreen, error) {
	return t.read(sessionID, terminalID, true)
}

func (t *Terminals) read(sessionID, terminalID string, styled bool) (TerminalScreen, error) {
	runtime, err := t.open()
	if err != nil {
		return TerminalScreen{}, err
	}
	defer runtime.Close()
	var terminal store.Session
	if sessionID == "" {
		terminal, err = runtime.unnestedChild(terminalKind, terminalID)
	} else if _, err = runtime.caller(sessionID); err == nil {
		terminal, err = runtime.child(terminalKind, terminalID)
	}
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
	info, err := runtime.info(terminal, true)
	if err != nil {
		return TerminalScreen{}, err
	}
	return TerminalScreen{
		Terminal: info,
		Output:   screenOutput(output, styled),
	}, nil
}
