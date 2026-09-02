// Package keybind is the vocabulary for keys written by name in
// config.toml: one spelling in, and the spelling each surface reads out of
// it, Bubble Tea's for the manager's own keyboard and tmux's for the
// bindings a managed session carries.
package keybind

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Key is one key as the user wrote it, resolved to what each side reads.
type Key struct {
	tea  string
	tmux string
}

// Tea is the name Bubble Tea reports for the key, the value of
// tea.KeyMsg.String().
func (k Key) Tea() string { return k.tea }

// Tmux is the name tmux binds the key under.
func (k Key) Tmux() string { return k.tmux }

func (k Key) String() string { return k.tea }

// Parse reads one key written as ctrl+<key>, alt+<key> or f1..f12. Inside a
// session every plain character belongs to the agent, so a key with no
// modifier is refused rather than bound.
func Parse(spec string) (Key, error) {
	name := strings.TrimSpace(spec)
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "ctrl+"):
		return ctrlKey(spec, lower[len("ctrl+"):])
	case strings.HasPrefix(lower, "alt+"):
		return altKey(spec, name[len("alt+"):])
	case strings.HasPrefix(lower, "f"):
		return functionKey(spec, lower[len("f"):])
	}
	return Key{}, fmt.Errorf("%q: a session key is ctrl+<key>, alt+<key> or f1..f12; a plain key reaches the agent", spec)
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

func functionKey(spec, rest string) (Key, error) {
	number, err := strconv.Atoi(rest)
	if err != nil || number < 1 || number > 12 {
		return Key{}, fmt.Errorf("%q: a session key is ctrl+<key>, alt+<key> or f1..f12; a plain key reaches the agent", spec)
	}
	return Key{tea: "f" + rest, tmux: "F" + rest}, nil
}

func isLetterOrDigit(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// Binding is the keys one action answers to. Written as "none" it holds no
// key and the action is off; left out of the file it is unset, and the
// default fills it.
type Binding struct {
	keys []Key
	set  bool
}

// Keys builds a binding from keys already parsed, which is how the defaults
// are written down.
func Keys(keys ...Key) Binding {
	return Binding{keys: keys, set: true}
}

// UnmarshalTOML accepts one key, a list of keys, or "none".
func (b *Binding) UnmarshalTOML(value any) error {
	b.set = true
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

// Has reports whether a key press answers to this binding; name is the
// value Bubble Tea reports for it.
func (b Binding) Has(name string) bool {
	for _, key := range b.keys {
		if key.tea == name {
			return true
		}
	}
	return false
}

// Label is the binding as a hint shows it: its keys separated by slashes,
// or nothing for an action that is off.
func (b Binding) Label() string {
	names := make([]string, 0, len(b.keys))
	for _, key := range b.keys {
		names = append(names, key.tea)
	}
	return strings.Join(names, " / ")
}

// Session is the table of keys the manager keeps for itself inside a
// managed session, attached or focused: everything else typed there goes
// to the agent.
type Session struct {
	Detach Binding `toml:"detach"`
	Review Binding `toml:"review"`
	Editor Binding `toml:"editor"`
}

func DefaultSession() Session {
	return Session{
		Detach: Keys(key("ctrl+q"), key(`ctrl+\`)),
		Review: Keys(key("ctrl+r")),
		Editor: Keys(key("f3")),
	}
}

func key(spec string) Key {
	parsed, err := Parse(spec)
	if err != nil {
		panic(err)
	}
	return parsed
}

// WithDefaults fills the actions the file left out. One written as "none"
// was a choice and stays empty.
func (s Session) WithDefaults() Session {
	defaults := DefaultSession()
	if !s.Detach.set {
		s.Detach = defaults.Detach
	}
	if !s.Review.set {
		s.Review = defaults.Review
	}
	if !s.Editor.set {
		s.Editor = defaults.Editor
	}
	return s
}

// Validate refuses a table with one key on two actions, and one with no
// way back: a focused session with no detach key has no exit.
func (s Session) Validate() error {
	if len(s.Detach.keys) == 0 {
		return errors.New("keybindings.session.detach needs at least one key: it is the way back from a focused session")
	}
	owners := map[string]string{}
	for _, action := range []struct {
		name    string
		binding Binding
	}{{"detach", s.Detach}, {"review", s.Review}, {"editor", s.Editor}} {
		for _, key := range action.binding.keys {
			if owner, taken := owners[key.tea]; taken {
				return fmt.Errorf("keybindings.session: %s is bound to both %s and %s", key, owner, action.name)
			}
			owners[key.tea] = action.name
		}
	}
	return nil
}
