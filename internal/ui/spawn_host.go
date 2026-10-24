package ui

import (
	"maps"
	"slices"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// spawnHost is the root as the New Session form, its choice and the quick
// bar read it. It answers questions only: every state change travels back
// as the request the package returns.
type spawnHost struct{ m *Model }

func (m *Model) spawnHost() spawnHost { return spawnHost{m: m} }

func (h spawnHost) ChoiceCaps(toolName string) spawn.Caps {
	tool := h.m.services.cfg.Tools[toolName]
	return spawn.Caps{
		Catalog:  tool.Catalog != "",
		Profiles: tool.ProfileArgs != "",
		Models:   tool.ModelArgs != "" || tool.ModelEnv != "",
		Efforts:  tool.EffortArgs != "",
	}
}

// ChoiceAnswer hands the choice a copy, so only the root's catalog
// handlers change what the CLI answered.
func (h spawnHost) ChoiceAnswer(toolName string) *spawn.Answer {
	answer := h.m.ledger.spawner.Answer(toolName)
	if answer == nil {
		return nil
	}
	return &spawn.Answer{Cat: answer.Cat, Err: answer.Err, Loaded: answer.Loaded, Loading: answer.Loading}
}

func (h spawnHost) ChoiceSetting(key string) string { return h.m.settings.Cache.Value(key) }

func (h spawnHost) SaveChoiceSetting(key, value string) { h.m.saveChoiceSetting(key, value) }

func (h spawnHost) ReportErr(text string) { h.m.reportErr(text) }

func (h spawnHost) ChoiceRemoteHost() string {
	m := h.m
	if f := m.form(); f != nil {
		return f.Remote.Host
	}
	if m.quick() != nil {
		if target, ok := m.remoteTarget(); ok {
			return target.host
		}
	}
	return ""
}

func (h spawnHost) ClearErr() { h.m.clearErr() }

func (h spawnHost) Prompt() spawn.Prompt {
	return promptBox{c: h.m.formPromptFor(), target: composer.Form, host: h.m}
}

func (h spawnHost) Paths() spawn.Paths {
	f := h.m.form()
	return spawn.PathField{
		State:  &f.Complete,
		Target: pathcomplete.Form,
		// A connection's directory is the host's: nothing here completes it.
		Off:    f.Remote.On(),
		Styles: pathCompleteStyles(h.m.prefs.glyphMode),
	}
}

// promptBox lends a feature package one of the root's prompt boxes.
type promptBox struct {
	c      *composer.Composer
	target composer.ID
	host   composer.Host
}

func (p promptBox) Focus() { p.c.Input.Focus() }

func (p promptBox) Blur() { p.c.Input.Blur() }

func (p promptBox) Fit(width, rows int) {
	p.c.Input.SetWidth(width)
	p.c.MaxRows = rows
}

func (p promptBox) View() string { return p.c.View(composerPaint()) }

func (p promptBox) StepRow(msg tea.KeyMsg) (tea.Cmd, bool) { return p.c.StepRow(msg) }

func (p promptBox) ChipKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	return p.c.ChipKey(p.host, p.target, msg)
}

func (p promptBox) Type(msg tea.KeyMsg) tea.Cmd { return p.c.TypeKey(msg) }

func (p promptBox) Release() { p.c.Release() }

func (p promptBox) Pasting() bool { return p.c.Pasting() }

func (p promptBox) Message() string { return p.c.Message() }

// spawnStyles copies the active theme for one paint.
func (m *Model) spawnStyles() spawn.Styles {
	return spawn.Styles{
		GlyphMode:   m.prefs.glyphMode,
		Subtle:      uitheme.SubtleStyle(),
		Value:       uitheme.ValueStyle(),
		Muted:       uitheme.MutedStyle(),
		Warn:        uitheme.WarnStyle(),
		Label:       uitheme.LabelStyle(),
		SubtleColor: uitheme.ColorSubtle(),
		Accent:      uitheme.ColorAccent(),
		Accent2:     uitheme.ColorAccent2(),
		Input:       uiframe.TextInputView,
	}
}

const formPromptMaxRows = 4

// promptField opens a New Session prompt box. The root numbers it as it puts
// the form on the stack.
func promptField() composer.Composer {
	in := textarea.New()
	in.CharLimit = 2000
	in.Placeholder = "first task (optional)"
	in.ShowLineNumbers = false
	in.SetPromptFunc(2, func(lineIndex int) string {
		if lineIndex == 0 {
			return "> "
		}
		return "  "
	})
	in.FocusedStyle.CursorLine = lipgloss.NewStyle()
	composer.HoldOpen(&in)
	return composer.Composer{Input: in, MaxRows: formPromptMaxRows}
}

// spawnDefaults are the root's launch inputs for a spawn into a group,
// read when the form or the quick bar builds its request.
type spawnDefaults struct {
	// base is the ref a worktree branches from, or "" to detect it.
	base string
	pane sessioncmd.PaneSize
	// groupDir is the group's own default path, and fallbacks the nearest
	// ancestor's and then the startup directory.
	groupDir  string
	fallbacks []string
	worktree  bool
}

func (m *Model) spawnDefaults(group string) spawnDefaults {
	paneW, paneH := m.paneTargetSize()
	return spawnDefaults{
		base:      m.groupBase(group),
		pane:      sessioncmd.PaneSize{Width: paneW, Height: paneH},
		groupDir:  m.workspace.groupPaths[group],
		fallbacks: m.groupDirCandidates(group),
		worktree:  m.cachedSpawnWorktreeDefault(group),
	}
}

// groupDirCandidates captures the same nearest-ancestor preference as the
// old synchronous lookup. Validation happens later inside the command.
func (m *Model) groupDirCandidates(group string) []string {
	candidates := make([]string, 0, 4)
	for current := group; current != ""; current = uirail.ParentGroup(current) {
		if path := m.workspace.groupPaths[current]; path != "" {
			candidates = append(candidates, path)
		}
	}
	if m.env.workDir != "" {
		candidates = append(candidates, m.env.workDir)
	}
	return candidates
}

func (m *Model) capturedGroupDefaultDir(group string) string {
	candidates := m.groupDirCandidates(group)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func (m *Model) cachedDefaultToolSelection() ([]string, int) {
	return m.cachedToolSelection(m.cachedHiddenTools(), "")
}

func (m *Model) cachedSpawnToolSelection() ([]string, int) {
	names, index := m.cachedDefaultToolSelection()
	for i, name := range names {
		if name == m.ledger.spawner.LastSpawnTool {
			return names, i
		}
	}
	return names, index
}

func (m *Model) cachedSpawnWorktreeDefault(group string) bool {
	for g := group; g != ""; g = uirail.ParentGroup(g) {
		switch m.workspace.groupWorktrees[g] {
		case "on":
			return true
		case "off":
			return false
		}
	}
	for _, name := range m.cachedEnabledToolNames() {
		if name == m.ledger.spawner.LastSpawnTool {
			return m.ledger.spawner.LastSpawnWorktree
		}
	}
	return m.settings.Cache.Value(uisettings.WorktreeSetting) == "on"
}

func (m *Model) cachedEnabledToolNames() []string {
	return m.services.cfg.EnabledAgentTools(m.cachedHiddenTools())
}

// groupBase is the ref a spawn into group branches from: the nearest ancestor
// group's choice, or "" to detect the repo's default branch.
func (m *Model) groupBase(group string) string {
	for g := group; g != ""; g = uirail.ParentGroup(g) {
		if base := m.workspace.groupBases[g]; base != "" {
			return base
		}
	}
	return ""
}

func (m *Model) cachedHiddenTools() map[string]bool {
	return m.settings.KnownHidden(m.services.cfg)
}

func (m *Model) cachedToolSelection(hidden map[string]bool, preferred string) ([]string, int) {
	return m.settings.ToolSelection(m.services.cfg, hidden, preferred)
}

// choiceSettingKeys are the per-CLI keys the form and quick prompt read from
// the settings cache.
func choiceSettingKeys(tools []string) []string { return spawn.SettingKeys(tools) }

// refreshFormChoicePrefs and refreshQuickChoicePrefs take a newer read of the
// CLI's kept choice into an open choice.
func (m *Model) refreshFormChoicePrefs() {
	f := m.form()
	f.Choice().RefreshPrefs(m.spawnHost(), f.Tool())
}

func (m *Model) refreshQuickChoicePrefs() {
	q := m.quick()
	q.Choice().RefreshPrefs(m.spawnHost(), q.Tool())
}

// groupPickerFor is the picker rows of the group tree, highlighting
// selectPath. Index 0 is always the root.
func (m *Model) groupPickerFor(selectPath string) spawn.Picker {
	groups, index := m.groupOptions(selectPath)
	return spawn.Picker{Groups: groups, GroupIndex: index}
}

// groupOptions is the picker's rows and the index of selectPath in them.
func (m *Model) groupOptions(selectPath string) ([]spawn.GroupOption, int) {
	sessionGroups := make([]string, 0, len(m.workspace.sessions))
	for _, session := range m.workspace.sessions {
		sessionGroups = append(sessionGroups, session.Group)
	}
	return spawn.Options(m.workspace.groups, sessionGroups, m.groupEffectivelyArchived, selectPath)
}

func (m *Model) viewGroupPicker(p *spawn.Picker) string {
	return spawn.ViewGroupOptions(p.Groups, p.GroupIndex, m.spawnStyles())
}

func (m *Model) capturedAncestorGroupDir(group string) string {
	for current := group; current != ""; current = uirail.ParentGroup(current) {
		if path := m.workspace.groupPaths[current]; path != "" {
			return path
		}
	}
	return ""
}

// stepGroupBase moves a group's base choice through auto and the branches of
// the repo at dir. Branches read within uilaunch.BaseRefsTTL step at once;
// otherwise the read runs as a command and the step lands with it.
func (m *Model) stepGroupBase(target uilaunch.BaseTarget, gen uint64, dir, current string, delta int) (string, tea.Cmd) {
	next, cmd, err := m.ledger.spawner.StepGroupBase(m.services.gitDrv, target, gen, dir, current, delta, time.Now())
	if err != nil {
		m.reportErr(err.Error())
		return current, nil
	}
	if cmd == nil {
		m.clearErr()
	}
	return next, cmd
}

func (m *Model) handleGroupBaseStep(msg uilaunch.BaseStepMsg) {
	var base *string
	switch msg.Target {
	case uilaunch.BaseForm:
		s, ok := surfaceOf[*uidialogs.GroupFormSurface](m)
		if !ok || uint64(s.Gen()) != msg.Gen || m.capturedAbsolutePath(s.Dir(), m.capturedGroupDefaultDir(s.Selected())) != msg.Dir {
			return
		}
		base = s.BaseRef()
	case uilaunch.BaseRename:
		s, ok := surfaceOf[*uidialogs.RenameSurface](m)
		if !ok || m.gens.dialog != msg.Gen || m.capturedAbsolutePath(s.Dir(), m.capturedGroupDefaultDir(uirail.ParentGroup(s.Path()))) != msg.Dir {
			return
		}
		base = &s.Base
	default:
		return
	}
	if msg.Err != nil {
		m.reportErr("group base: " + msg.Err.Error())
		return
	}
	m.ledger.spawner.KeepBaseRefs(msg.Dir, msg.Refs, time.Now())
	if *base != msg.From {
		return
	}
	m.clearErr()
	*base = uilaunch.StepBaseChoice(msg.Refs, msg.From, msg.Delta)
}

// refreshSpawnBase resolves the base of the worktree spawn the form or the
// quick bar is set to make, for the form to show, then fetches it unless
// Settings turned that off. A spawn that beats the fetch branches from the
// last one.
func (m *Model) refreshSpawnBase() tea.Cmd {
	dir, group, ok := m.pendingWorktreeSpawn()
	if !ok {
		return nil
	}
	key := uilaunch.BaseFetchKey{Dir: dir, Override: m.groupBase(group)}
	if !m.ledger.spawner.ReserveBaseFetch(key, time.Now()) {
		return nil
	}
	return uilaunch.ResolveBaseCmd(m.services.gitDrv, key)
}

// recordBaseFetch keeps what a step of a base refresh found, and starts the
// fetch once the resolving step is in.
func (m *Model) recordBaseFetch(msg uilaunch.BaseFetchedMsg) tea.Cmd {
	return m.ledger.spawner.RecordBaseFetch(msg, m.services.gitDrv, m.prefs.baseFetchOff)
}

// pendingWorktreeSpawn is the directory and group of the worktree spawn the
// New Session form or the quick bar is set to make.
func (m *Model) pendingWorktreeSpawn() (dir, group string, ok bool) {
	switch {
	case m.form() != nil && m.formWorktreeOn():
		return m.formSpawnDir(), m.selectedGroupPath(), true
	case m.quick() != nil && m.quickSpawning() && m.quickWorktreeOn():
		return m.quickTargetDir(), m.quickTargetGroup(), true
	}
	return "", "", false
}

func (m *Model) groupEffectivelyArchived(path string) bool {
	return store.EffectivelyArchived(m.workspace.archivedGroups, path)
}

// applyCachedFormDefaults takes the loaded tool and worktree defaults. A
// tool the load moved to starts its choice rows over and asks its CLI.
func (m *Model) applyCachedFormDefaults() tea.Cmd {
	f := m.form()
	before := f.Tool()
	f.SetTools(m.cachedSpawnToolSelection())
	f.DefaultWorktree(m.cachedSpawnWorktreeDefault(m.selectedGroupPath()))
	toolName := f.Tool()
	if toolName == before || toolName == "" {
		return nil
	}
	f.RestartChoice(m.spawnHost())
	m.syncFormFieldWidths()
	return m.ensureCatalog(toolName)
}

// fitChoices refits every open choice to a newer catalog answer.
func (m *Model) fitChoices() {
	if f := m.form(); f != nil {
		f.Choice().Refit(m.spawnHost(), f.Tool())
	}
	if q := m.quick(); q != nil {
		q.Choice().Refit(m.spawnHost(), q.Tool())
	}
}

func (m *Model) saveChoiceSetting(key, value string) {
	m.settings.Cache.ApplyValues([]uisettings.Value{{Key: key, Value: value}})
	m.enqueueEffect(uilaunch.ChoiceSaveRequest{Key: key, Value: value}, 0, false)
}

func (m *Model) choiceSettingKeys() []string {
	return choiceSettingKeys(slices.Sorted(maps.Keys(m.services.cfg.Tools)))
}
