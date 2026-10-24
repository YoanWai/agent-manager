package ui

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) openForm() tea.Cmd {
	return m.openFormWithReader(uipersist.StoreWriter{Store: m.services.store})
}

func (m *Model) openFormWithReader(reader uipersist.Reader) tea.Cmd {
	tools, toolIndex := m.cachedSpawnToolSelection()
	if len(tools) == 0 {
		m.reportErr("no CLIs enabled: open settings (s), then CLIs, to turn some on")
		return nil
	}
	prompt := promptField()
	prompt.Gen = m.nextComposerGen()
	m.closeQuick()
	f := m.newSpawnSurface(&prompt, pathcomplete.State{Generation: uint64(prompt.Gen) << 32}, spawn.NewForm(m.spawnHost(), tools, toolIndex), m.groupPickerFor(m.contextGroup()))
	m.push(f)
	m.clearErr()
	m.syncFormFieldWidths()
	m.forgetWorktreeCapability()
	f.SetDir(m.capturedGroupDefaultDir(m.selectedGroupPath()))
	f.SeedWorktree(m.cachedSpawnWorktreeDefault(m.selectedGroupPath()))
	if target, ok := m.remoteTarget(); ok {
		f.AimRemote(spawn.Remote{Host: target.host, Group: target.group})
		f.RestartChoice(m.spawnHost())
	}
	catalog := m.ensureCatalog(tools[toolIndex])
	saved := uipersist.SavedCmd(m.services.store, uipersist.SavedForForm, uint64(prompt.Gen), 0)
	if m.settings.Pending > 0 {
		return tea.Batch(m.formWorktreeProbeCmd(false), catalog, saved)
	}
	return tea.Batch(uipersist.LoadCmd(uipersist.LoadRequest{Target: uipersist.FormTarget, Generation: uint64(prompt.Gen), Extra: m.choiceSettingKeys()}, reader), catalog, saved)
}

// applyCachedFormDefaults takes the loaded tool and worktree defaults. A
// tool the load moved to starts its choice rows over and asks its CLI.
func (m *Model) formTool() string { return m.form().Tool() }

// selectedGroupPath is the group the open picker highlights: the move
// card's, or the New Session form's.
func (m *Model) selectedGroupPath() string {
	if s, ok := surfaceOf[*uidialogs.MoveSurface](m); ok {
		return s.Picker().GroupPath()
	}
	if f := m.form(); f != nil {
		return f.GroupPath()
	}
	return ""
}

func (m *Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, request := m.form().HandleKey(m.spawnHost(), composer.TypedText(msg))
	return m, tea.Batch(cmd, m.runFormRequest(request, systemDirectoryPreflight{git: m.services.gitDrv}))
}

// runFormRequest executes what a form key or click asked of the root.
func (m *Model) runFormRequest(request spawn.FormRequest, reader directoryPreflight) tea.Cmd {
	if cmd, taken := m.remoteFormRequest(request); taken {
		return cmd
	}
	switch {
	case request.Close:
		m.closeForm()
	case request.ApplyPath:
		return m.applyPathSuggestion()
	case request.PickPath:
		if m.form().Complete.Pick(request.PathEntry) {
			return m.applyPathSuggestion()
		}
	case request.Group != 0:
		return m.moveGroupCursor(request.Group)
	case request.PickGroup:
		if delta := request.GroupEntry - m.form().GroupIndex; delta != 0 {
			return m.moveGroupCursor(delta)
		}
	case request.Toggle:
		return m.toggleFormWorktree()
	case request.Probe:
		return m.formWorktreeProbeCmd(false)
	case request.Catalog != "":
		m.syncFormFieldWidths()
		return m.ensureCatalog(request.Catalog)
	case request.Spawn != nil:
		req := m.formSpawnRequest(*request.Spawn)
		req.DirReader = crossPreflight(reader)
		m.clearErr()
		m.dispatchSpawn(req)
		return m.nextEffectCmd()
	}
	return nil
}

func (m *Model) handleFormClick(line int) (tea.Model, tea.Cmd) {
	return m, m.runFormRequest(m.form().Form.Click(m.spawnHost(), line), nil)
}

func (m *Model) formWheel(step int) tea.Cmd {
	return m.runFormRequest(m.form().Form.Wheel(m.spawnHost(), step), nil)
}

// moveGroupCursor moves within the form's group picker, wrapping at the
// ends; a delta of 0 re-resolves the dependent defaults in place.
func (m *Model) moveGroupCursor(delta int) tea.Cmd {
	f := m.form()
	if !f.Step(delta) {
		return nil
	}
	if f.DirAuto() {
		f.SetDir(m.capturedGroupDefaultDir(f.GroupPath()))
	}
	f.DefaultWorktree(m.cachedSpawnWorktreeDefault(f.GroupPath()))
	return m.formWorktreeProbeCmd(false)
}

func (m *Model) submitForm() (tea.Model, tea.Cmd) {
	return m.submitFormWithReader(systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) submitFormWithReader(reader directoryPreflight) (tea.Model, tea.Cmd) {
	return m, m.runFormRequest(m.form().Submit(m.spawnHost()), reader)
}

// formValueWidth is the columns a field value can occupy inside the card.
func (m *Model) formValueWidth() int {
	return uiframe.CardInnerWidth(m.cardWidth()) - spawn.LabelColumn
}

// syncFormFieldWidths fits the fields to the card so long values scroll
// (inputs) or wrap (prompt) instead of clipping at the card edge.
func (m *Model) syncFormFieldWidths() {
	f := m.form()
	if f == nil {
		return
	}
	inner := m.formValueWidth()
	f.SyncWidths(inner)
	m.formPromptFor().Input.SetWidth(inner)
}

func (m *Model) viewForm() string {
	f := m.form()
	facts := spawn.FormFacts{
		Width:  m.formValueWidth(),
		NoRepo: uilaunch.WorktreeUnavailable,
		Group:  presentation.EscapeControlsInline(uiframe.DisplayGroup(f.GroupPath())),
		Styles: m.spawnStyles(),
		Height: m.layout.height,
		Chrome: m.cardChromeRows,
	}
	if f.Focused() == spawn.FieldGroup {
		facts.GroupPicker = strings.Split(m.viewGroupPicker(&f.Picker), "\n")
	}
	facts.WorktreeCapable, facts.WorktreeKnown = m.cachedWorktreeCapability(m.formSpawnDir())
	if f.Remote.On() {
		facts.WorktreeCapable, facts.WorktreeKnown = true, true
		facts.Host = lipgloss.NewStyle().Foreground(uitheme.ColorRemote()).Render(f.Remote.Host)
	} else if m.formWorktreeOn() {
		facts.Base = m.spawnBaseLabel(m.formSpawnDir(), m.selectedGroupPath())
	}
	body, hint := f.Form.View(m.spawnHost(), facts)
	return m.CardSized(m.cardWidth(), m.glyphs().NewSessionGlyph+" New Session", body, hint)
}

func (m *Model) rememberSpawnPick(tool string, worktree bool) {
	m.ledger.spawner.RememberSpawn(tool, worktree)
}

func (m *Model) routeSpawnMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case uilaunch.ProbeMsg:
		return routed(m.handleWorktreeProbe(msg))

	case pathcomplete.Msg:
		m.completer(msg.Request.Target).Handle(pathCompleter{m}, msg)
		return routed(m, nil)

	case terminalDirectoryMsg:
		return routed(m.handleTerminalDirectory(msg))

	case uidialogs.GroupFormSubmitMsg:
		return routed(m, m.handleGroupFormSubmit(msg, systemDirectoryPreflight{git: m.services.gitDrv}))

	case uidialogs.GroupBaseAskMsg:
		return routed(m, m.handleGroupBaseAsk(msg))
	}
	return nil, nil, false
}

// spawnFormRoot is the root as the New Session form reaches it, holding the
// prompt box that cannot live in the package: ui/composer imports ui/spawn,
// so the form's prompt box stays on the root.
type spawnFormRoot struct {
	*Model
	prompt *composer.Composer
}

func (r spawnFormRoot) FormKey(msg tea.KeyMsg) tea.Cmd {
	_, cmd := r.handleFormKey(msg)
	return cmd
}

func (r spawnFormRoot) FormClick(line int) tea.Cmd {
	_, cmd := r.handleFormClick(line)
	return cmd
}

func (r spawnFormRoot) FormWheel(step int) tea.Cmd { return r.formWheel(step) }

func (r spawnFormRoot) FormView() string { return r.viewForm() }

func (r spawnFormRoot) TakeForeground() { r.takeForeground() }

func (r spawnFormRoot) ReleasePrompt() { r.prompt.Release() }

// newSpawnSurface builds the New Session form over the root's own prompt
// box, once the root has the completer state it starts from.
func (m *Model) newSpawnSurface(prompt *composer.Composer, complete pathcomplete.State, form spawn.Form, picker spawn.Picker) *spawn.Surface {
	return spawn.NewSurface(spawnFormRoot{m, prompt}, form, picker, complete)
}

// form is the open New Session form, nil when none is on the stack.
func (m *Model) form() *spawn.Surface {
	s, _ := surfaceOf[*spawn.Surface](m)
	return s
}

// formPromptFor is the prompt box of the open New Session form, nil when
// none is up.
func (m *Model) formPromptFor() *composer.Composer {
	f := m.form()
	if f == nil {
		return nil
	}
	return f.Root().(spawnFormRoot).prompt
}

// formFor is the open form when it is the one composerGen came from.
func (m *Model) formFor(composerGen int) (*spawn.Surface, bool) {
	s := m.form()
	return s, s != nil && s.Root().(spawnFormRoot).prompt.Gen == composerGen
}

// formHolds is whether the form composerGen came from still shows draft
// and no image: a spawn of exactly that prompt may take its place.
func (m *Model) formHolds(composerGen int, draft string) bool {
	f, ok := m.formFor(composerGen)
	if !ok {
		return false
	}
	prompt := f.Root().(spawnFormRoot).prompt
	return prompt.Input.Value() == draft && len(prompt.Attachments) == 0
}

// formDraftHolds is formHolds for a spawn that also named a name and a
// directory: only the form that dispatched it may be closed.
func (m *Model) formDraftHolds(composerGen int, draft, name, dir string) bool {
	f, ok := m.formFor(composerGen)
	return ok && m.formHolds(composerGen, draft) && f.Name() == name && f.Dir() == dir
}

// formPromptForGen is the prompt box of the form composerGen came from.
func (m *Model) formPromptForGen(composerGen int) (*composer.Composer, bool) {
	f, ok := m.formFor(composerGen)
	if !ok {
		return nil, false
	}
	return f.Root().(spawnFormRoot).prompt, true
}

// closeForm takes the form off the stack wherever it sits: a spawn can land
// under a card opened since.
func (m *Model) closeForm() {
	for i := len(m.surfaces) - 1; i >= 0; i-- {
		if s, ok := m.surfaces[i].(*spawn.Surface); ok {
			m.surfaces = append(m.surfaces[:i:i], m.surfaces[i+1:]...)
			s.Leave(m)
			return
		}
	}
}

var _ spawn.SurfaceRoot = spawnFormRoot{}

// ensureCatalog reads the kept answer or asks the CLI off the update path,
// since an answer takes up to seconds.
func (m *Model) ensureCatalog(toolName string) tea.Cmd {
	cmd, _ := m.ledger.spawner.EnsureCatalog(m.services.cfg, m.services.configDir, toolName, time.Now())
	return cmd
}

func (m *Model) handleCatalog(msg uilaunch.CatalogMsg) tea.Cmd {
	outdated, cmd := m.ledger.spawner.Apply(msg, m.services.cfg, m.services.configDir, time.Now())
	m.fitChoices()
	if outdated != nil && m.catalogToolActive(msg.Tool) {
		m.advanceDialogGen()
		command := uiinstall.OutdatedCommand(*outdated)
		m.presentLaunchHint(uiinstall.OutdatedFix(*outdated, command))
	}
	return cmd
}

// catalogToolActive reports whether the form or the quick bar shows the CLI
// whose answer this is, which is what earns the update dialog.
func (m *Model) catalogToolActive(tool string) bool {
	return (m.form() != nil && m.formTool() == tool) || (m.quick() != nil && m.quickTool() == tool)
}

// formSpawnRequest names the session, places it in the picked group and
// captures the group's launch inputs and the prompt's images.
func (m *Model) formSpawnRequest(request spawn.Request) uilaunch.Request {
	f := m.form()
	prompt := m.formPromptFor()
	defaults := m.spawnDefaults(f.GroupPath())
	return uilaunch.FormRequest(request, f.Name(), f.GroupPath(), prompt, defaults)
}

// quickSpawnRequest names the session and captures the group's launch
// inputs, the worktree default it keeps unless toggled, and the prompt's
// images.
func (m *Model) quickSpawnRequest(request spawn.Request) uilaunch.Request {
	q := m.quick()
	worktree := request.Worktree
	if !request.WorktreeSet {
		worktree = m.cachedSpawnWorktreeDefault(request.Group)
	}
	request.Worktree = worktree
	return uilaunch.QuickRequest(request, &q.Composer, m.spawnDefaults(request.Group))
}

// formSpawnDir is the directory the form would launch in, resolved the
// same way submit resolves it.
func (m *Model) formSpawnDir() string {
	return m.capturedAbsolutePath(m.form().Dir(), m.env.workDir)
}

// capturedAbsolutePath normalizes a displayed path without consulting the
// filesystem. It uses the startup directory/home captured before the event
// loop began.
func (m *Model) capturedAbsolutePath(raw, fallback string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		path = fallback
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		path = filepath.Join(m.env.homeDir, strings.TrimPrefix(path, "~"))
	}
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(m.env.workDir, path)
	}
	return filepath.Clean(path)
}

// dispatchSpawn takes the prompt's images out of the composer and queues the
// spawn on the shared effect lane; completion routes the images.
func (m *Model) dispatchSpawn(request uilaunch.Request) {
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if pending, ok := job.Request.(uilaunch.Request); ok && pending.Kind == request.Kind && pending.ComposerGen == request.ComposerGen {
			m.reportErr("this spawn is already in progress")
			return
		}
	}
	request.ID = newID()
	switch request.Kind {
	case uilaunch.Form:
		f := m.form()
		prompt := m.formPromptFor()
		request.Draft = prompt.Input.Value()
		request.DraftName = f.Name()
		request.DraftDir = f.Dir()
		prompt.Attachments = nil
	case uilaunch.Quick:
		q := m.quick()
		request.Draft = q.Input.Value()
		q.Attachments = nil
	}
	m.enqueueEffect(request, 0, false)
}

func (m *Model) materializeGroupsLocal(paths []string) {
	known := make(map[string]bool, len(m.workspace.groups))
	for _, path := range m.workspace.groups {
		known[path] = true
	}
	for _, path := range paths {
		if !known[path] {
			m.workspace.groups = append(m.workspace.groups, path)
			known[path] = true
		}
	}
}

// applyGroupEffect applies a created group: the durable inventory takes the
// row wherever the user is, and only the form that dispatched it gives up
// the screen.
func (m *Model) applyGroupEffect(request uilaunch.GroupRequest, result uilaunch.GroupResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	m.materializeGroupsLocal([]string{result.Path})
	if m.workspace.groupPaths == nil {
		m.workspace.groupPaths = map[string]string{}
	}
	m.workspace.groupPaths[result.Path] = result.Dir
	m.workspace.groupWorktrees = setGroupChoice(m.workspace.groupWorktrees, result.Path, result.Worktree)
	m.workspace.groupBases = setGroupChoice(m.workspace.groupBases, result.Path, request.Base)
	m.rebuildRows()
	// Presentation (closing the form, revealing and selecting the new group)
	// lands only when the dispatching group form is still the dialog on
	// screen; a newer one keeps the foreground. The reveal's collapse
	// snapshot queues after the row exists, the order the synchronous submit
	// kept.
	if s, ok := surfaceOf[*uidialogs.GroupFormSurface](m); ok && s.Gen() == request.Gen && s.Name() == request.DraftName && s.Dir() == request.DraftDir {
		m.clearErr()
		m.Pop()
		m.queueRail(m.rail.RevealGroup(result.Path).Mutations, 0, true)
	}
	return m.refreshCmd()
}

// spawnBaseLabel names the ref a worktree spawn into dir branches from, where
// that choice came from, and how fetching it went.
func (m *Model) spawnBaseLabel(dir, group string) string {
	override := m.groupBase(group)
	fetch := m.ledger.spawner.BaseFetch(dir, override)
	label := uitheme.ValueStyle().Render(override) + uitheme.SubtleStyle().Render(" (group)")
	if override == "" {
		switch {
		case !fetch.Resolved:
			label = uitheme.SubtleStyle().Render("…")
		case fetch.Detected == "":
			label = uitheme.ValueStyle().Render("HEAD") + uitheme.SubtleStyle().Render(" (auto)")
		default:
			label = uitheme.ValueStyle().Render(fetch.Detected) + uitheme.SubtleStyle().Render(" (auto)")
		}
	}
	switch {
	case m.prefs.baseFetchOff:
	case !fetch.Fetched:
		label += uitheme.SubtleStyle().Render(" · fetching")
	case fetch.Err != nil:
		label += uitheme.SubtleStyle().Render(" · fetch failed")
	}
	return label
}

// launchServices is the effect services as a spawn reaches them.
func (s effectServices) launchServices() uilaunch.Services {
	return uilaunch.Services{Lifecycle: s.lifecycle, Store: s.store, Config: s.cfg, Git: s.gitDrv}
}

// applySpawn applies a finished spawn: a committed row joins the workspace,
// and a refused one hands its prompt back to the dialog that still shows it.
func (m *Model) applySpawn(request uilaunch.Request, result uilaunch.Result, err error) tea.Cmd {
	if result.RepoKnown {
		m.ledger.spawner.RememberWorktree(result.Dir, result.RepoCapable, time.Now())
	}
	if err == nil {
		m.commitSpawn(request, result.Session)
		if result.Label != nil {
			m.reportErr(result.Label.Error())
		}
		return m.refreshCmd()
	}
	request.Plan = result.Plan
	request.Dir = result.Dir
	request.Worktree = result.Worktree
	if request.Kind == uilaunch.Shell {
		m.ledger.lifecycle.TerminalKeyAt = time.Now()
		m.reportErr(err.Error())
		return nil
	}
	// Nothing was committed. A spawn the hint dialog refuses takes the
	// dialog's place, the same way the synchronous submit did; a dialog the
	// user opened since the dispatch is newer and keeps the screen.
	if !m.spawnTakesOver(request) {
		m.returnSpawnImages(request)
		m.reportErr(err.Error())
		return nil
	}
	m.reportLaunchError(err)
	if fix, ok := m.launchHintFix(); ok {
		// The dialog's images are the request's: the composer was emptied at
		// dispatch, so openLaunchHint found none of its own.
		fix.Images = append(fix.Images, request.Images...)
		fix.Retry = request
		if request.Kind == uilaunch.Quick {
			m.closeQuick()
		}
	} else {
		// A plain error left the composer on screen; its images go back.
		m.returnSpawnImages(request)
	}
	return nil
}

func (m *Model) commitSpawn(request uilaunch.Request, sess store.Session) {
	m.markFreshPane(sess.ID)
	m.ledger.lifecycle.RecordLaunch(sess.ID, time.Now())
	m.workspace.sessions = append(m.workspace.sessions, sess)
	// The directive went out with the launch, so the row waits for the name
	// the agent picks instead of showing the one generated for it.
	if request.AutoNamed {
		m.ledger.lifecycle.AwaitRename(sess.ID, sess.Name, request.Prompt)
	}
	if request.Kind != uilaunch.Shell {
		m.ledger.spawner.RememberSpawn(request.ToolName, request.PickWorktree)
		spawn.RememberModel(m.spawnHost(), request.ToolName, spawn.Pick(request.Choice))
	}
	// New sessions start as starting, which attention excludes; clear so the
	// row the spawn just created is on screen.
	m.rail.ClearStatusFilter()
	switch request.Kind {
	case uilaunch.Form:
		// A form opened later is a newer dialog the completion must not
		// close; only the one that dispatched this spawn is closed.
		if m.formDraftHolds(request.ComposerGen, request.Draft, request.DraftName, request.DraftDir) {
			m.closeForm()
		}
	case uilaunch.Quick:
		if q, ok := m.quickFor(request.ComposerGen); ok {
			m.clearAccepted(request.ComposerGen, request.Draft, q.CloseAfterSend)
		}
	}
	m.rebuildRows()
	if request.Kind == uilaunch.Shell {
		m.ledger.lifecycle.TerminalKeyAt = time.Now()
		m.focusSession(sess.ID)
	}
}

// spawnTakesOver reports whether a refused spawn may replace the screen: the
// dialog that dispatched it still shows its prompt, or the list is bare.
func (m *Model) spawnTakesOver(request uilaunch.Request) bool {
	switch {
	case request.Kind == uilaunch.Form && m.formHolds(request.ComposerGen, request.Draft):
		return true
	case request.Kind == uilaunch.Quick && m.quickHolds(request.ComposerGen, request.Draft):
		return true
	}
	return m.listClear()
}

// returnSpawnImages gives the prompt's images back to the composer that is
// still on screen, so the user can fix and resubmit; when no such composer
// remains, the files are dropped.
func (m *Model) returnSpawnImages(request uilaunch.Request) {
	switch request.Kind {
	case uilaunch.Form:
		if prompt, ok := m.formPromptForGen(request.ComposerGen); ok {
			prompt.Attachments = append(request.Images, prompt.Attachments...)
			return
		}
	case uilaunch.Quick:
		if q, ok := m.quickFor(request.ComposerGen); ok {
			q.Attachments = append(request.Images, q.Attachments...)
			return
		}
	}
	uiinstall.DropImages(request.Images)
}

// formWorktreeOn is the worktree state the form shows and spawns with: the
// toggle, unless the chosen directory cannot host a worktree.
func (m *Model) formWorktreeOn() bool {
	capable, known := m.cachedWorktreeCapability(m.formSpawnDir())
	return m.form().Worktree() && known && capable
}

// toggleFormWorktree flips the toggle, or explains why the chosen directory
// rules a worktree out.
func (m *Model) toggleFormWorktree() tea.Cmd {
	dir := m.formSpawnDir()
	capable, known := m.cachedWorktreeCapability(dir)
	if !known {
		return m.formWorktreeProbeCmd(true)
	}
	if !capable {
		m.reportErr("worktree sessions need a git repository: " + dir + " is not one")
		return nil
	}
	m.clearErr()
	f := m.form()
	f.SetWorktree(!f.Worktree())
	return nil
}

func (m *Model) cachedWorktreeCapability(dir string) (bool, bool) {
	return m.ledger.spawner.CachedWorktree(dir, time.Now())
}

func (m *Model) requestDirectoryProbe(target uilaunch.ProbeTarget, generation int, dir string, toggle bool, from bool) tea.Cmd {
	m.gens.worktreeProbe++
	request := uilaunch.ProbeRequest{
		Target: target, Generation: generation, Sequence: m.gens.worktreeProbe,
		Dir: dir, Toggle: toggle, From: from,
	}
	return uilaunch.ProbeCmd(request, preflightReader{reader: systemDirectoryPreflight{git: m.services.gitDrv}})
}

func (m *Model) formWorktreeProbeCmd(toggleOn bool) tea.Cmd {
	f := m.form()
	if f.Remote.On() {
		return nil
	}
	return m.requestDirectoryProbe(uilaunch.ProbeForm, m.formPromptFor().Gen, m.formSpawnDir(), toggleOn, f.Worktree())
}

func (m *Model) quickWorktreeProbeCmd(toggleOn bool) tea.Cmd {
	if _, remote := m.remoteTarget(); remote {
		return nil
	}
	q := m.quick()
	return m.requestDirectoryProbe(uilaunch.ProbeQuick, q.Gen, m.quickTargetDir(), toggleOn, q.Worktree())
}

func (m *Model) handleWorktreeProbe(msg uilaunch.ProbeMsg) (tea.Model, tea.Cmd) {
	request := msg.Request
	if request.Sequence != m.gens.worktreeProbe {
		return m, nil
	}
	switch request.Target {
	case uilaunch.ProbeForm:
		if _, ok := m.formFor(request.Generation); !ok || m.formSpawnDir() != request.Dir {
			return m, nil
		}
	case uilaunch.ProbeQuick:
		if _, ok := m.quickFor(request.Generation); !ok || m.quickTargetDir() != request.Dir {
			return m, nil
		}
	default:
		return m, nil
	}
	m.ledger.spawner.RememberWorktree(request.Dir, msg.Capable, time.Now())
	if !request.Toggle {
		return m, nil
	}
	if !msg.Capable {
		m.reportErr("worktree sessions need a git repository: " + request.Dir + " is not one")
		return m, nil
	}
	m.clearErr()
	if request.Target == uilaunch.ProbeForm {
		if m.form().Worktree() != request.From {
			return m, nil
		}
		m.form().SetWorktree(!request.From)
	} else {
		if m.quick().Worktree() != request.From {
			return m, nil
		}
		m.quick().SetWorktree(!request.From)
	}
	return m, nil
}

// forgetWorktreeCapability drops the memo so the next look is a fresh one.
// Opening the form or the quick bar calls it.
func (m *Model) forgetWorktreeCapability() { m.ledger.spawner.ForgetWorktree() }
