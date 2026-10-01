package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

func bindingOf(t *testing.T, specs ...string) keybind.Binding {
	t.Helper()
	keys := make([]keybind.Key, 0, len(specs))
	for _, spec := range specs {
		key, err := keybind.Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		keys = append(keys, key)
	}
	return keybind.Keys(keys...)
}

// openBeside opens the store of a config dir that holds this config.toml,
// the state an upgrade from a release that read the file starts in.
func openBeside(t *testing.T, configFile string) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(configFile), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return reopen(t, dir), dir
}

func reopen(t *testing.T, dir string) *Store {
	t.Helper()
	st, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func sessionKeys(t *testing.T, st *Store) keybind.Table {
	t.Helper()
	keys, err := st.SessionKeys()
	if err != nil {
		t.Fatalf("SessionKeys: %v", err)
	}
	return keys
}

func listKeys(t *testing.T, st *Store) keybind.Table {
	t.Helper()
	keys, err := st.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	return keys
}

func TestAFreshStoreBindsTheDefaultsAndWritesNoFile(t *testing.T) {
	dir := t.TempDir()
	st := reopen(t, dir)
	if !sessionKeys(t, st).Equal(keybind.DefaultSession()) {
		t.Error("session keys are not the defaults")
	}
	if !listKeys(t, st).Equal(keybind.DefaultList()) {
		t.Error("list keys are not the defaults")
	}
	if editor, err := st.Editor(); err != nil || editor != "" {
		t.Errorf("editor = %q, %v", editor, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("a first run must not write config.toml: %v", err)
	}
}

func TestSetKeysRoundTripsBothTables(t *testing.T) {
	st := newTestStore(t)
	session := keybind.DefaultSession().
		With(keybind.Detach, bindingOf(t, "f9", "alt+q")).
		With(keybind.Review, bindingOf(t))
	list := keybind.DefaultList().
		With(keybind.NewSession, bindingOf(t, "N")).
		With(keybind.Prompt, bindingOf(t, "space", "p")).
		With(keybind.Quit, bindingOf(t))
	for _, keys := range []keybind.Table{session, list} {
		if err := st.SetKeys(keys); err != nil {
			t.Fatalf("SetKeys %s: %v", keys.Scope(), err)
		}
	}
	if !sessionKeys(t, st).Equal(session) {
		t.Error("session keys changed on the way through the store")
	}
	if !listKeys(t, st).Equal(list) {
		t.Error("list keys changed on the way through the store")
	}
}

// Only a moved key is stored. An action at its default stays unnamed, so
// a default a later release changes still reaches it.
func TestSetKeysStoresOnlyTheKeysThatMoved(t *testing.T) {
	st := newTestStore(t)
	moved := keybind.DefaultSession().With(keybind.Review, bindingOf(t, "alt+r"))
	if err := st.SetKeys(moved); err != nil {
		t.Fatalf("SetKeys: %v", err)
	}
	if raw, err := st.Setting(keysSetting(keybind.ScopeSession)); err != nil || raw != `{"review":["alt+r"]}` {
		t.Fatalf("stored keys = %s, %v", raw, err)
	}
	if err := st.SetKeys(moved.With(keybind.Review, bindingOf(t, "ctrl+r"))); err != nil {
		t.Fatalf("SetKeys back to the default: %v", err)
	}
	if raw, err := st.Setting(keysSetting(keybind.ScopeSession)); err != nil || raw != `{}` {
		t.Fatalf("a key moved back should leave no name, got %s, %v", raw, err)
	}
}

func TestOpenImportsOnlyTheKeysTheFileMoved(t *testing.T) {
	st, _ := openBeside(t, "[keybindings.session]\ndetach = [\"ctrl+q\", \"ctrl+\\\\\"]\nreview = \"alt+r\"\n\n[keybindings.list]\nquit = \"q\"\n")
	if raw, err := st.Setting(keysSetting(keybind.ScopeSession)); err != nil || raw != `{"review":["alt+r"]}` {
		t.Fatalf("stored session keys = %s, %v", raw, err)
	}
	if raw, err := st.Setting(keysSetting(keybind.ScopeList)); err != nil || raw != "" {
		t.Fatalf("a list table at its defaults should not be stored, got %s, %v", raw, err)
	}
}

func TestSetKeysRefusesATableWithNoWayBack(t *testing.T) {
	st := newTestStore(t)
	err := st.SetKeys(keybind.DefaultSession().With(keybind.Detach, bindingOf(t)))
	if err == nil || !strings.Contains(err.Error(), "detach needs at least one key") {
		t.Fatalf("err = %v, want the detach rule", err)
	}
	if !sessionKeys(t, st).Equal(keybind.DefaultSession()) {
		t.Fatal("a refused table must leave the stored one alone")
	}
}

// The upgrade path: a file that names keys keeps them, an action it left
// out keeps yielding to them, and the file itself is not rewritten.
func TestOpenImportsTheKeysAndEditorOfAnExistingFile(t *testing.T) {
	file := `poll_interval = "5s"
editor = "code -n"

[tools.claude]
command = "claude"

[keybindings.session]
detach = "ctrl+s"
review = "none"

[keybindings.list]
archive = "y"
fork = "ctrl+f"
`
	st, dir := openBeside(t, file)

	session := sessionKeys(t, st)
	if got := session.Binding(keybind.Detach).Label(); got != "ctrl+s" {
		t.Errorf("detach = %q, want the file's ctrl+s", got)
	}
	if got := session.Binding(keybind.Review).Label(); got != "" {
		t.Errorf("review = %q, want it off as the file had it", got)
	}
	if got := session.Binding(keybind.Editor).Label(); got != "f3" {
		t.Errorf("editor key = %q, want the default the file left alone", got)
	}
	list := listKeys(t, st)
	for key, want := range map[string]string{"y": keybind.Archive, "ctrl+f": keybind.Fork, "?": keybind.Help} {
		if got, _ := list.ActionFor(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if len(list.Binding(keybind.CopyReply).Keys()) != 0 {
		t.Error("copy_reply holds y only by default, so it must yield it")
	}
	if editor, err := st.Editor(); err != nil || editor != "code -n" {
		t.Errorf("editor = %q, %v", editor, err)
	}
	if reason, err := st.ConfigImportError(); err != nil || reason != "" {
		t.Errorf("import error = %q, %v", reason, err)
	}
	if onDisk, err := os.ReadFile(filepath.Join(dir, "config.toml")); err != nil || string(onDisk) != file {
		t.Fatalf("the file should be left as it was, got %v:\n%s", err, onDisk)
	}
}

// What Settings stored afterwards wins: the file is read once.
func TestTheFileIsImportedOnce(t *testing.T) {
	st, dir := openBeside(t, "editor = \"code\"\n\n[keybindings.session]\ndetach = \"ctrl+s\"\n")
	moved := sessionKeys(t, st).With(keybind.Detach, bindingOf(t, "f9"))
	if err := st.SetKeys(moved); err != nil {
		t.Fatalf("SetKeys: %v", err)
	}
	if err := st.SetEditor(""); err != nil {
		t.Fatalf("SetEditor: %v", err)
	}
	later := "editor = \"zed\"\n\n[keybindings.session]\ndetach = \"f5\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(later), 0o644); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	again := reopen(t, dir)
	if got := sessionKeys(t, again).Binding(keybind.Detach).Label(); got != "f9" {
		t.Errorf("detach = %q, want the key Settings stored", got)
	}
	if editor, err := again.Editor(); err != nil || editor != "" {
		t.Errorf("editor = %q, %v, want the cleared one", editor, err)
	}
}

// A file the import cannot read does not lock the user out: the store
// opens on the defaults and keeps the reason for the manager to show.
func TestAFileThatCannotBeImportedLeavesTheDefaultsAndTheReason(t *testing.T) {
	st, dir := openBeside(t, "[keybindings.session]\ndetach = \"none\"\n")
	if !sessionKeys(t, st).Equal(keybind.DefaultSession()) {
		t.Error("a refused file should leave the default keys")
	}
	reason, err := st.ConfigImportError()
	if err != nil || !strings.Contains(reason, "detach needs at least one key") || !strings.Contains(reason, "config.toml") {
		t.Fatalf("import error = %q, %v", reason, err)
	}

	fixed := "[keybindings.session]\ndetach = \"f9\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(fixed), 0o644); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if !sessionKeys(t, reopen(t, dir)).Equal(keybind.DefaultSession()) {
		t.Error("the import runs once, a refused file included")
	}
}

// A table stored before tmux_prefix existed loads it off, and the keys the
// picker gives it come back as they went in.
func TestTheTmuxPrefixRoundTripsThroughTheStore(t *testing.T) {
	st := newTestStore(t)
	older := `{"detach":["ctrl+s"],"review":["ctrl+r"],"editor":["f3"]}`
	if err := st.SetSetting(keysSetting(keybind.ScopeSession), older); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	loaded := sessionKeys(t, st)
	if got := loaded.Binding(keybind.TmuxPrefix).Label(); got != "" {
		t.Fatalf("an older table should leave tmux_prefix off, got %q", got)
	}
	keys := loaded.With(keybind.TmuxPrefix, bindingOf(t, "ctrl+b", "f12"))
	if err := st.SetKeys(keys); err != nil {
		t.Fatalf("SetKeys: %v", err)
	}
	if !sessionKeys(t, st).Equal(keys) {
		t.Fatalf("reloaded tmux_prefix = %q", sessionKeys(t, st).Binding(keybind.TmuxPrefix).Label())
	}
	if err := st.SetKeys(keys.With(keybind.TmuxPrefix, bindingOf(t, "ctrl+b", "f12", "f11"))); err == nil || !strings.Contains(err.Error(), "takes one key or two") {
		t.Fatalf("err = %v, want the two-key rule", err)
	}
}

// A file that set tmux_prefix, as one key or as tmux's prefix and prefix2,
// keeps it through the import.
func TestOpenImportsTheTmuxPrefix(t *testing.T) {
	for written, want := range map[string]string{
		`tmux_prefix = "ctrl+b"`:          "ctrl+b",
		`tmux_prefix = ["ctrl+b", "f12"]`: "ctrl+b / f12",
	} {
		st, _ := openBeside(t, "[keybindings.session]\ndetach = \"ctrl+s\"\n"+written+"\n")
		keys := sessionKeys(t, st)
		if got := keys.Binding(keybind.TmuxPrefix).Label(); got != want {
			t.Errorf("%s imported as %q, want %q", written, got, want)
		}
		if got := keys.Binding(keybind.Detach).Label(); got != "ctrl+s" {
			t.Errorf("%s: detach = %q", written, got)
		}
	}
}

// Two releases can share one store. A table saved by the one with more
// actions still loads here, and saving here keeps what it stored.
func TestKeysOfANewerReleaseSurviveThisOne(t *testing.T) {
	st := newTestStore(t)
	stored := `{"detach":["ctrl+s"],"from_a_newer_release":["f12"]}`
	if err := st.SetSetting(keysSetting(keybind.ScopeSession), stored); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	keys := sessionKeys(t, st)
	if got := keys.Binding(keybind.Detach).Label(); got != "ctrl+s" {
		t.Fatalf("detach = %q", got)
	}
	if err := st.SetKeys(keys.With(keybind.Review, bindingOf(t, "alt+r"))); err != nil {
		t.Fatalf("SetKeys: %v", err)
	}
	raw, err := st.Setting(keysSetting(keybind.ScopeSession))
	if err != nil || !strings.Contains(raw, `"from_a_newer_release":["f12"]`) || !strings.Contains(raw, `"review":["alt+r"]`) {
		t.Fatalf("stored keys = %s, %v", raw, err)
	}
}
