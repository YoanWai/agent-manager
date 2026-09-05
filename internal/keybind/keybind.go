// Package keybind is the vocabulary for keys written by name in
// config.toml: one spelling in, and the spelling each surface reads out of
// it, Bubble Tea's for the manager's own keyboard and tmux's for the
// bindings a managed session carries. A Table is one scope's actions and
// the keys each answers to.
package keybind

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Key struct {
	tea  string
	tmux string
}

// Tea is the value tea.KeyMsg.String() reports for the key.
func (k Key) Tea() string { return k.tea }

// Tmux is the key as a tmux binding takes it, empty for a key tmux cannot
// bind: a plain key belongs to the program in the pane.
func (k Key) Tmux() string { return k.tmux }

func (k Key) String() string { return k.tea }

var glyphs = map[string]string{
	"enter": "↵", "up": "↑", "down": "↓", "left": "←", "right": "→",
	"shift+up": "shift+↑", "shift+down": "shift+↓", "shift+left": "shift+←", "shift+right": "shift+→",
	"backspace": "⌫",
}

// Glyph is the key as the footer and the key map draw it.
func (k Key) Glyph() string {
	if glyph, drawn := glyphs[k.tea]; drawn {
		return glyph
	}
	return k.tea
}

var namedKeys = []string{"space", "enter", "tab", "backspace", "delete", "up", "down", "left", "right", "home", "end", "pgup", "pgdn"}

// Normalize folds the spellings a terminal has for one key into the one a
// table stores: a shifted letter is its capital, and the space bar is
// named rather than typed.
func Normalize(tea string) string {
	if tea == " " {
		return "space"
	}
	if rest, shifted := strings.CutPrefix(tea, "shift+"); shifted && len(rest) == 1 && rest[0] >= 'a' && rest[0] <= 'z' {
		return strings.ToUpper(rest)
	}
	return tea
}

// Parse reads one key written by name: a character, a key name, ctrl+<key>,
// alt+<key>, shift+<arrow> or f1..f12. esc and ctrl+c are not keys a table
// may take, since esc cancels everywhere and ctrl+c always quits.
func Parse(spec string) (Key, error) {
	// The bare space bar is named before trimming would erase it.
	name := Normalize(strings.TrimSpace(Normalize(spec)))
	lower := strings.ToLower(name)
	switch {
	case name == "":
		return Key{}, errors.New("a key is empty; write a character, a key name, ctrl+<key>, alt+<key> or f1..f12")
	case lower == "esc" || lower == "ctrl+c":
		return Key{}, fmt.Errorf("%q stays as it is: esc cancels and ctrl+c quits everywhere", spec)
	case utf8.RuneCountInString(name) == 1:
		return Key{tea: name}, nil
	case strings.HasPrefix(lower, "ctrl+"):
		return ctrlKey(spec, lower[len("ctrl+"):])
	case strings.HasPrefix(lower, "alt+"):
		return altKey(spec, name[len("alt+"):])
	case strings.HasPrefix(lower, "shift+"):
		return shiftKey(spec, lower[len("shift+"):])
	}
	for _, named := range namedKeys {
		if lower == named {
			return Key{tea: named}, nil
		}
	}
	if strings.HasPrefix(lower, "f") {
		return functionKey(spec, lower[len("f"):])
	}
	return Key{}, notAKey(spec)
}

func notAKey(spec string) error {
	return fmt.Errorf("%q is not a key the manager reads; write a character, one of %s, ctrl+<key>, alt+<key>, shift+<arrow> or f1..f12", spec, strings.Join(namedKeys, " "))
}

// ctrlKey accepts the control characters the terminal can send: the
// letters and the five symbols that share the range with them. Three of
// those arrive as another key entirely and are refused under that name.
func ctrlKey(spec, rest string) (Key, error) {
	if len(rest) != 1 {
		return Key{}, fmt.Errorf("%q: ctrl+ takes one letter or one of @ \\ ] ^ _", spec)
	}
	switch rest {
	case "i":
		return Key{}, fmt.Errorf("%q: ctrl+i is tab, which the agent needs", spec)
	case "m":
		return Key{}, fmt.Errorf("%q: ctrl+m is enter, which the agent needs", spec)
	case "[":
		return Key{}, fmt.Errorf("%q: ctrl+[ is escape, which the agent needs", spec)
	}
	if !(rest[0] >= 'a' && rest[0] <= 'z') && !strings.Contains(`@\]^_`, rest) {
		return Key{}, fmt.Errorf("%q: ctrl+ takes one letter or one of @ \\ ] ^ _", spec)
	}
	return Key{tea: "ctrl+" + rest, tmux: "C-" + rest}, nil
}

// altKey takes a letter or a digit. Punctuation is out because tmux reads
// several of those marks as its own syntax when it parses the binding.
func altKey(spec, rest string) (Key, error) {
	if len(rest) != 1 || !isLetterOrDigit(rest[0]) {
		return Key{}, fmt.Errorf("%q: alt+ takes one letter or digit", spec)
	}
	return Key{tea: "alt+" + rest, tmux: "M-" + rest}, nil
}

// shiftKey covers the keys a terminal reports with the modifier spelled
// out; a shifted letter arrives as its capital and Normalize already
// turned it into one.
func shiftKey(spec, rest string) (Key, error) {
	switch rest {
	case "up", "down", "left", "right", "tab":
		return Key{tea: "shift+" + rest}, nil
	}
	return Key{}, fmt.Errorf("%q: shift+ takes an arrow or tab; a shifted letter is written as its capital", spec)
}

func functionKey(spec, rest string) (Key, error) {
	number, err := strconv.Atoi(rest)
	if err != nil || number < 1 || number > 12 || rest != strconv.Itoa(number) {
		return Key{}, notAKey(spec)
	}
	return Key{tea: "f" + rest, tmux: "F" + rest}, nil
}

func isLetterOrDigit(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// Binding is the keys one action answers to. Written as "none" it holds no
// key and the action is off.
type Binding struct {
	keys []Key
}

func Keys(keys ...Key) Binding {
	return Binding{keys: keys}
}

func (b *Binding) UnmarshalTOML(value any) error {
	b.keys = nil
	switch spec := value.(type) {
	case string:
		if strings.EqualFold(strings.TrimSpace(spec), "none") {
			return nil
		}
		key, err := Parse(spec)
		if err != nil {
			return err
		}
		b.keys = []Key{key}
	case []any:
		for _, item := range spec {
			text, ok := item.(string)
			if !ok {
				return fmt.Errorf("%v: a key is written as a string", item)
			}
			key, err := Parse(text)
			if err != nil {
				return err
			}
			if b.Has(key.tea) {
				return fmt.Errorf("%s is listed twice", key)
			}
			b.keys = append(b.keys, key)
		}
	default:
		return fmt.Errorf("a binding is a key, a list of keys or \"none\", not %v", value)
	}
	return nil
}

func (b Binding) Keys() []Key { return b.keys }

func (b Binding) Has(name string) bool {
	for _, key := range b.keys {
		if key.tea == name {
			return true
		}
	}
	return false
}

// Label is the keys as they are written, for the picker and the file.
func (b Binding) Label() string {
	names := make([]string, 0, len(b.keys))
	for _, key := range b.keys {
		names = append(names, key.tea)
	}
	return strings.Join(names, " / ")
}

// Glyph is the keys as the footer and the key map draw them.
func (b Binding) Glyph(separator string) string {
	names := make([]string, 0, len(b.keys))
	for _, key := range b.keys {
		names = append(names, key.Glyph())
	}
	return strings.Join(names, separator)
}

// Action is one thing a scope can be told to do, named the way the file
// names it.
type Action struct {
	Name     string
	Does     string
	defaults Binding
}

// The action names, shared by every place that reads a table.
const (
	Detach      = "detach"
	Review      = "review"
	Editor      = "editor"
	Quit        = "quit"
	Up          = "up"
	Down        = "down"
	ReorderUp   = "reorder_up"
	ReorderDown = "reorder_down"
	Open        = "open"
	Attach      = "attach"
	StepIn      = "step_in"
	StepOut     = "step_out"
	NewSession  = "new_session"
	Terminal    = "terminal"
	NewGroup    = "new_group"
	Fork        = "fork"
	Prompt      = "prompt"
	MarkIdle    = "mark_idle"
	Rename      = "rename"
	Move        = "move"
	Restart     = "restart"
	Kill        = "kill"
	KillAll     = "kill_all"
	Revive      = "revive"
	ReviveAll   = "revive_all"
	Archive     = "archive"
	Restore     = "restore"
	Delete      = "delete"
	Search      = "search"
	Filter      = "filter"
	Archived    = "archived"
	EmptyGroups = "empty_groups"
	FoldAll     = "fold_all"
	Resize      = "resize"
	Settings    = "settings"
	Messages    = "messages"
	Help        = "help"
)

const (
	ScopeSession = "session"
	ScopeList    = "list"
)

var sessionActions = []Action{
	{Detach, "back to the manager", keys("ctrl+q", `ctrl+\`)},
	{Review, "open the session's diff", keys("ctrl+r")},
	{Editor, "open its directory", keys("f3")},
}

var listActions = []Action{
	{Up, "move the cursor up", keys("up", "k")},
	{Down, "move the cursor down", keys("down", "j")},
	{Open, "session: focus it (or attach); group: fold it", keys("enter")},
	{Attach, "session: attach it (or focus)", keys("A")},
	{StepIn, "step in: focus the session, open the group", keys("right")},
	{StepOut, "step out: close the group", keys("left")},
	{ReorderUp, "move the row up among its siblings", keys("shift+up", "K")},
	{ReorderDown, "move the row down among its siblings", keys("shift+down", "J")},
	{NewSession, "new session", keys("n")},
	{Terminal, "new terminal tab", keys("T")},
	{NewGroup, "new group", keys("g")},
	{Fork, "fork the session", keys("f")},
	{Prompt, "quick prompt", keys("space")},
	{Review, "review the session's diff", keys("ctrl+r")},
	{MarkIdle, "mark a finished session idle", keys(".")},
	{Rename, "rename the session, edit the group", keys("r")},
	{Move, "move the row to another group", keys("m")},
	{Editor, "open the directory in your editor", keys("o")},
	{Restart, "restart the session on an empty context", keys("R")},
	{Kill, "kill the session, or every live one in the group", keys("x")},
	{KillAll, "kill every live session in view", keys("X")},
	{Revive, "revive the session, or every dead one in the group", keys("v")},
	{ReviveAll, "revive every dead session in view", keys("V")},
	{Archive, "archive the session or group", keys("a")},
	{Restore, "restore the session or group", keys("u")},
	{Delete, "delete the session or group", keys("d")},
	{Search, "search the list", keys("/")},
	{Filter, "filter to what needs attention", keys("w")},
	{Archived, "archived view", keys("t")},
	{EmptyGroups, "hide / show empty groups", keys("e")},
	{FoldAll, "fold / unfold every group", keys("F")},
	{Resize, "resize the split", keys("|")},
	{Settings, "settings", keys("s")},
	{Messages, "messages", keys("M")},
	{Help, "the key map", keys("?")},
	{Quit, "quit (sessions keep running)", keys("q")},
}

func keys(specs ...string) Binding {
	parsed := make([]Key, 0, len(specs))
	for _, spec := range specs {
		key, err := Parse(spec)
		if err != nil {
			panic(err)
		}
		parsed = append(parsed, key)
	}
	return Keys(parsed...)
}

// Table is one scope's actions and the keys each answers to: the keys the
// manager keeps inside a session, or the keys of its own list.
type Table struct {
	scope   string
	actions []Action
	bound   map[string]Binding
}

func DefaultSession() Table {
	table, _ := build(ScopeSession, sessionActions, nil)
	return table
}

func DefaultList() Table {
	table, _ := build(ScopeList, listActions, nil)
	return table
}

// SessionTable is the session table a file declares: an action left out
// keeps its default, and a table that could not work is refused.
func SessionTable(written map[string]Binding) (Table, error) {
	return build(ScopeSession, sessionActions, written)
}

func ListTable(written map[string]Binding) (Table, error) {
	return build(ScopeList, listActions, written)
}

func build(scope string, actions []Action, written map[string]Binding) (Table, error) {
	table := Table{scope: scope, actions: actions, bound: make(map[string]Binding, len(actions))}
	for _, action := range actions {
		table.bound[action.Name] = action.defaults
	}
	names := make([]string, 0, len(written))
	for name := range written {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, known := table.bound[name]; !known {
			return Table{}, fmt.Errorf("keybindings.%s: no action named %q; the actions are %s", scope, name, table.names())
		}
		table.bound[name] = written[name]
	}
	if err := table.Validate(); err != nil {
		return Table{}, err
	}
	return table, nil
}

func (t Table) names() string {
	names := make([]string, 0, len(t.actions))
	for _, action := range t.actions {
		names = append(names, action.Name)
	}
	return strings.Join(names, ", ")
}

func (t Table) Scope() string { return t.scope }

func (t Table) Actions() []Action { return t.actions }

func (t Table) Binding(name string) Binding { return t.bound[name] }

// With is the table with one action on other keys; the receiver is left
// as it was.
func (t Table) With(name string, binding Binding) Table {
	bound := make(map[string]Binding, len(t.bound))
	for action, keys := range t.bound {
		bound[action] = keys
	}
	bound[name] = binding
	return Table{scope: t.scope, actions: t.actions, bound: bound}
}

func (t Table) Defaults() Table {
	table, _ := build(t.scope, t.actions, nil)
	return table
}

func (t Table) Equal(other Table) bool {
	if t.scope != other.scope {
		return false
	}
	for _, action := range t.actions {
		if t.bound[action.Name].Label() != other.bound[action.Name].Label() {
			return false
		}
	}
	return true
}

// ActionFor is the action a pressed key answers to, if any.
func (t Table) ActionFor(key string) (string, bool) {
	for _, action := range t.actions {
		if t.bound[action.Name].Has(key) {
			return action.Name, true
		}
	}
	return "", false
}

// Validate refuses a table with one key on two actions, and one with no
// way back: a focused session with no detach key has no exit, and a list
// with no settings key has no way to the picker. Inside a session every
// plain key belongs to the agent, so only a key tmux can bind is taken.
func (t Table) Validate() error {
	switch t.scope {
	case ScopeSession:
		if len(t.bound[Detach].keys) == 0 {
			return errors.New("keybindings.session.detach needs at least one key: it is the way back from a focused session")
		}
		for _, action := range t.actions {
			for _, key := range t.bound[action.Name].keys {
				if key.tmux == "" {
					return fmt.Errorf("keybindings.session.%s: %q is a plain key, which reaches the agent; a session key is ctrl+<key>, alt+<key> or f1..f12", action.Name, key)
				}
			}
		}
	case ScopeList:
		if len(t.bound[Settings].keys) == 0 {
			return errors.New("keybindings.list.settings needs at least one key: it is the way back to the key picker")
		}
	}
	owners := map[string]string{}
	for _, action := range t.actions {
		for _, key := range t.bound[action.Name].keys {
			if owner, taken := owners[key.tea]; taken {
				return fmt.Errorf("keybindings.%s: %s is bound to both %s and %s", t.scope, key, owner, action.Name)
			}
			owners[key.tea] = action.Name
		}
	}
	return nil
}
