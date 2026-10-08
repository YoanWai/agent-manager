package ui

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type blockedDirectoryPreflight struct {
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
	resolved string
	dirOK    bool
	repoOK   bool
}

func (r *blockedDirectoryPreflight) resolve(string, []string) (string, bool) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.resolved, r.dirOK
}

func (r *blockedDirectoryPreflight) repoCapable(string) bool { return r.repoOK }

func TestSpawnWorkerDefersFilesystemReadAndCapturesDraft(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.form.name.SetValue("captured")
	m.form.dir.SetValue("/captured/raw")
	reader := &blockedDirectoryPreflight{
		started: make(chan struct{}), release: make(chan struct{}),
		resolved: t.TempDir(), dirOK: true,
	}
	_, cmd := m.submitFormWithReader(reader)
	if m.effects.active == nil {
		t.Fatal("submit did not accept the spawn before directory validation")
	}
	select {
	case <-reader.started:
		t.Fatal("submit read the filesystem on the update path")
	default:
	}

	completed := make(chan effectCompletedMsg, 1)
	go func() { completed <- cmd().(effectCompletedMsg) }()
	<-reader.started
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 151, Height: 45})
	m = updated.(*Model)
	if m.layout.width != 151 {
		t.Fatal("blocked preflight blocked an unrelated update")
	}
	close(reader.release)
	msg := <-completed
	request := m.effects.active.request.(spawnRequest)
	if request.draftName != "captured" || request.rawDir != "/captured/raw" {
		t.Fatalf("worker did not capture the draft: %+v", request)
	}
	m.applyTestMsg(t, msg)
	if len(m.workspace.sessions) != 1 || m.workspace.sessions[0].Name != "captured" {
		t.Fatalf("accepted spawn did not commit the captured row: %+v", m.workspace.sessions)
	}
}

func TestAcceptedSpawnSurvivesChangedDraftWithoutClosingIt(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.form.name.SetValue("first")
	reader := &blockedDirectoryPreflight{
		started: make(chan struct{}), release: make(chan struct{}),
		resolved: t.TempDir(), dirOK: true,
	}
	_, cmd := m.submitFormWithReader(reader)
	completed := make(chan effectCompletedMsg, 1)
	go func() { completed <- cmd().(effectCompletedMsg) }()
	<-reader.started
	m.form.name.SetValue("newer")
	close(reader.release)
	m.applyTestMsg(t, <-completed)
	if len(m.workspace.sessions) != 1 || m.workspace.sessions[0].Name != "first" {
		t.Fatalf("accepted spawn was cancelled or retargeted: %+v", m.workspace.sessions)
	}
	if m.mode != modeForm || m.form.name.Value() != "newer" {
		t.Fatal("completion replaced the edited form")
	}
}

func TestQuickPreflightCapturesTargetBeforeCursorMoves(t *testing.T) {
	m := buildModel(t)
	backend := t.TempDir()
	frontend := t.TempDir()
	if err := m.services.store.CreateGroup("backend", backend); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.CreateGroup("frontend", frontend); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")
	m.openQuickMode()
	m.quick.input.SetValue("captured prompt")
	reader := &blockedDirectoryPreflight{
		started: make(chan struct{}), release: make(chan struct{}),
		resolved: backend, dirOK: true,
	}
	_, cmd := m.quickSpawnWithReader("backend", "captured prompt", reader)
	completed := make(chan effectCompletedMsg, 1)
	go func() { completed <- cmd().(effectCompletedMsg) }()
	<-reader.started
	m.selectGroupRow(t, "frontend")
	close(reader.release)
	m.applyTestMsg(t, <-completed)
	if len(m.workspace.sessions) != 1 || m.workspace.sessions[0].Group != "backend" {
		t.Fatalf("quick preflight retargeted the spawn: %+v", m.workspace.sessions)
	}
}

func TestGroupPreflightFailureKeepsFormAndDoesNotMutate(t *testing.T) {
	m := buildModel(t)
	m.openGroupForm()
	m.groupForm.name.SetValue("docs")
	m.groupForm.path.SetValue("/missing/docs")
	reader := &blockedDirectoryPreflight{
		started: make(chan struct{}), release: make(chan struct{}),
		resolved: "/missing/docs", dirOK: false,
	}
	_, cmd := m.submitGroupFormWithReader(reader)
	close(reader.release)
	m.applyTestMsg(t, cmd())
	if m.mode != modeGroupForm || m.effects.active != nil || len(m.effects.pending) != 0 {
		t.Fatal("failed group validation closed the form or left a mutation queued")
	}
	if got := m.errBar.text; got != "default path does not exist: /missing/docs" {
		t.Fatalf("group preflight error = %q", got)
	}
}

func TestAcceptedGroupDrainsBlockedValidationOnQuit(t *testing.T) {
	m := buildModel(t)
	m.openGroupForm()
	m.groupForm.name.SetValue("accepted")
	reader := &blockedDirectoryPreflight{
		started: make(chan struct{}), release: make(chan struct{}),
		resolved: t.TempDir(), dirOK: true,
	}
	_, cmd := m.submitGroupFormWithReader(reader)
	if m.effects.active == nil {
		t.Fatal("group submission was not accepted before validation")
	}
	if _, quit := m.requestQuit(); quit != nil {
		t.Fatal("quit bypassed the accepted group worker")
	}
	completed := make(chan tea.Msg, 1)
	go func() { completed <- cmd() }()
	<-reader.started
	close(reader.release)
	m.applyTestMsg(t, <-completed)
	if !hasWorkspaceGroup(m, "accepted") {
		t.Fatal("quit discarded the accepted group")
	}
}

// submitFormSpawn fills the form, accepts its spawn effect, and returns its
// still-unrun worker command.
func submitFormSpawn(t *testing.T, m *Model, name, dir string) tea.Cmd {
	t.Helper()
	m.openForm()
	m.form.name.SetValue(name)
	m.form.dir.SetValue(dir)
	m.form.toolIndex = 0
	_, command := m.submitForm()
	if m.effects.active == nil {
		t.Fatal("submit did not queue the spawn worker")
	}
	return command
}

func storeRows(t *testing.T, m *Model) int {
	t.Helper()
	rows, err := m.services.store.ListSessions(false)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	return len(rows)
}

// The form submit must return to the user before the spawn's writer runs:
// no row in the workspace, no row in the store, no pane, no ledger entry,
// and the form still open. The committed session appears only when the
// queued effect completes.
func TestSpawnFormDefersCreationFromUpdate(t *testing.T) {
	m := buildModel(t)
	cmd := submitFormSpawn(t, m, "first", t.TempDir())
	if m.errBar.text != "" {
		t.Fatalf("submit reported an error: %q", m.errBar.text)
	}
	if m.mode != modeForm {
		t.Fatalf("form closed at submit; mode=%v", m.mode)
	}
	if len(m.workspace.sessions) != 0 {
		t.Fatalf("workspace gained %d rows on the update path", len(m.workspace.sessions))
	}
	if got := storeRows(t, m); got != 0 {
		t.Fatalf("store gained %d rows on the update path", got)
	}
	if len(m.ledger.launched) != 0 {
		t.Fatalf("ledger recorded %d launches on the update path", len(m.ledger.launched))
	}
	m.applyCmd(t, cmd)
	if got := storeRows(t, m); got != 1 {
		t.Fatalf("store rows after completion = %d, want 1", got)
	}
	if len(m.workspace.sessions) != 1 {
		t.Fatalf("workspace rows after completion = %d, want 1", len(m.workspace.sessions))
	}
	sess := m.workspace.sessions[0]
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("pane missing after completion")
	}
	if m.mode != modeList {
		t.Fatalf("mode after completion = %v, want list", m.mode)
	}
}

// Quick spawn on a selected group defers the same way: nothing exists
// between the key press and the effect completion.
func TestQuickSpawnDefersCreationFromUpdate(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range railRows(m) {
		if row.isGroup && row.group == "backend" {
			setRailCursor(m, i)
		}
	}
	m.openQuickMode()
	m.quick.input.SetValue("build the api")
	_, cmd := m.submitQuick()
	if m.errBar.text != "" {
		t.Fatalf("quick spawn reported an error: %q", m.errBar.text)
	}
	if len(m.workspace.sessions) != 0 || storeRows(t, m) != 0 {
		t.Fatalf("quick spawn created a row on the update path: workspace=%d store=%d",
			len(m.workspace.sessions), storeRows(t, m))
	}
	if len(m.ledger.launched) != 0 {
		t.Fatal("ledger recorded a launch on the update path")
	}
	m.applyCmd(t, cmd)
	sessions := m.sessionRows()
	if len(sessions) != 1 {
		t.Fatalf("sessions after completion = %d, want 1", len(sessions))
	}
	if !m.services.tmux.Exists(sessions[0].ID) {
		t.Fatal("pane missing after completion")
	}
	if m.quick.input.Value() != "" {
		t.Fatal("quick bar was not cleared after the spawn completed")
	}
}

// Group creation persists only when the effect completes; the form stays
// open until then.
func TestGroupFormDefersCreationFromUpdate(t *testing.T) {
	m := buildModel(t)
	m.openGroupForm()
	m.groupForm.name.SetValue("manual")
	m.groupForm.path.SetValue(t.TempDir())
	_, cmd := m.submitGroupForm()
	if m.errBar.text != "" {
		t.Fatalf("group form reported an error: %q", m.errBar.text)
	}
	if m.mode != modeGroupForm {
		t.Fatalf("group form closed at submit; mode=%v", m.mode)
	}
	for _, g := range m.groupRowPaths() {
		if strings.HasSuffix(g, "manual") {
			t.Fatalf("group %q materialized on the update path", g)
		}
	}
	m.applyCmd(t, cmd)
	groups, err := m.services.store.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	found := false
	for _, g := range groups {
		if strings.HasSuffix(g.Name, "manual") {
			found = true
		}
	}
	if !found {
		t.Fatal("group not persisted after completion")
	}
	if m.mode != modeList {
		t.Fatalf("mode after completion = %v, want list", m.mode)
	}
}

// A worker-level failure leaves nothing durable behind: no row, no ledger
// entry, and the form stays open for the fix.
func TestSpawnWorkerFailureLeavesNothingBehind(t *testing.T) {
	m := buildModel(t)
	cmd := submitFormSpawn(t, m, "first", t.TempDir())
	// Close the store before the effect runs: the worker's first read fails
	// and nothing is created.
	m.services.store.Close()
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("spawn with an unopenable store should report an error")
	}
	if len(m.workspace.sessions) != 0 {
		t.Fatal("failed spawn left a workspace row")
	}
	if len(m.ledger.launched) != 0 {
		t.Fatal("ledger recorded a launch for a failed spawn")
	}
	if m.mode != modeForm {
		t.Fatalf("form should stay open after a failed spawn; mode=%v", m.mode)
	}
}

// A dispatch captures the selected identity and inputs; later navigation
// cannot retarget an accepted spawn. The completion applies the captured
// group and prompt, not whatever the cursor points at by then.
func TestSpawnCompletesAgainstCapturedIdentity(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range railRows(m) {
		if row.isGroup && row.group == "backend" {
			setRailCursor(m, i)
		}
	}
	m.openQuickMode()
	m.quick.input.SetValue("captured prompt")
	_, cmd := m.submitQuick()
	// A newer selection lands before the effect completes: it must not
	// retarget the accepted spawn.
	if err := m.services.store.CreateGroup("frontend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range railRows(m) {
		if row.isGroup && row.group == "frontend" {
			setRailCursor(m, i)
		}
	}
	m.applyCmd(t, cmd)
	sessions := m.sessionRows()
	if len(sessions) != 1 {
		t.Fatalf("sessions after completion = %d, want 1", len(sessions))
	}
	if sessions[0].Group != "backend" {
		t.Fatalf("spawn landed in %q, want the captured group backend", sessions[0].Group)
	}
}

// A window update must return while a spawn is queued: the writer runs only
// when the effect command runs, never on the update path.
func TestSpawnUpdateReturnsWhileWriterIsQueued(t *testing.T) {
	m := buildModel(t)
	submitFormSpawn(t, m, "first", t.TempDir())
	// The effect is queued, not begun: no row, no pane, and a window update
	// must not block on it.
	if storeRows(t, m) != 0 || len(m.ledger.launched) != 0 {
		t.Fatal("writer ran on the update path")
	}
	done := make(chan struct{})
	go func() {
		m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("window update waited on a queued spawn")
	}
	if storeRows(t, m) != 0 {
		t.Fatal("window update started the spawn writer")
	}
}

// A completion applies exactly once: a duplicate completion message must be
// a no-op, adding no second row and advancing no observation fence.
func TestSpawnDuplicateCompletionIsIdempotent(t *testing.T) {
	m := buildModel(t)
	submitFormSpawn(t, m, "first", t.TempDir())
	msg := m.effects.active.command()
	m.applyTestMsg(t, msg)
	fence := m.effects.latestObservation
	rows := storeRows(t, m)
	m.applyTestMsg(t, msg)
	if got := storeRows(t, m); got != rows {
		t.Fatalf("duplicate completion added a row: %d -> %d", rows, got)
	}
	if got := len(m.workspace.sessions); got != rows {
		t.Fatalf("duplicate completion added a workspace row: %d, want %d", got, rows)
	}
	if m.effects.latestObservation != fence {
		t.Fatal("duplicate completion advanced the observation fence")
	}
}

// An in-band quit drains an accepted spawn before emitting tea.Quit; a
// dispatch that arrives after the quit is refused, not run.
func TestSpawnDrainsAcceptedOnQuitAndRefusesLate(t *testing.T) {
	m := buildModel(t)
	accepted := submitFormSpawn(t, m, "first", t.TempDir())
	_, command := m.requestQuit()
	if command != nil {
		t.Fatal("quit started a second command while the accepted spawn was active")
	}
	m.applyCmd(t, accepted)
	if storeRows(t, m) != 1 {
		t.Fatalf("accepted spawn did not drain on quit: %d rows", storeRows(t, m))
	}
	// quitting is still set; a new dispatch is refused, not run.
	m.openForm()
	m.form.name.SetValue("late")
	m.form.dir.SetValue(t.TempDir())
	m.form.toolIndex = 0
	_, late := m.submitForm()
	m.applyTestMsg(t, late())
	if len(m.effects.pending) != 0 {
		t.Fatal("quit accepted a new spawn")
	}
}

// A failed spawn must not replace a dialog the user opened since the
// dispatch: the newer screen keeps the foreground and the form's images
// are dropped rather than handed to a dead composer.
func TestSpawnFailureDoesNotReplaceNewerDialog(t *testing.T) {
	m := buildModel(t)
	cmd := submitFormSpawn(t, m, "first", t.TempDir())
	// The user closes this form and opens a fresh one in the same slot: a
	// newer dialog the completion must not close.
	m.form.prompt.release()
	m.mode = modeList
	m.openForm()
	m.form.name.SetValue("second")
	m.form.dir.SetValue(t.TempDir())
	m.form.toolIndex = 0
	newGen := m.form.prompt.gen
	m.services.store.Close()
	m.applyCmd(t, cmd)
	if m.mode != modeForm {
		t.Fatalf("the newer form was closed; mode=%v", m.mode)
	}
	if m.form.prompt.gen != newGen {
		t.Fatal("the newer form was replaced by the completion")
	}
	if m.errBar.text == "" {
		t.Fatal("the failure should surface in the bar")
	}
}

// A group creation that completes after the user reopened the group form
// must reconcile the durable inventory without closing the newer form or
// stealing its selection.
func TestGroupFormCompletionDoesNotReplaceNewerDialog(t *testing.T) {
	m := buildModel(t)
	m.openGroupForm()
	m.groupForm.name.SetValue("first")
	m.groupForm.path.SetValue(t.TempDir())
	_, cmd := m.submitGroupForm()
	if m.effects.active == nil {
		t.Fatal("accepted group submission did not queue the mutation")
	}
	// The user reopens the group form before the first one completes: a
	// newer dialog in the same slot.
	m.openGroupForm()
	m.groupForm.name.SetValue("second")
	m.groupForm.path.SetValue(t.TempDir())
	m.applyCmd(t, cmd)
	if m.mode != modeGroupForm {
		t.Fatalf("completion replaced the newer group form; mode=%v", m.mode)
	}
	if _, ok := m.workspace.groupPaths["first"]; !ok {
		t.Fatal("durable group inventory was not reconciled")
	}
	if got := m.groupRowPaths(); !containsPath(got, "first") {
		t.Fatalf("new group row missing from inventory: %v", got)
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

// A successful spawn keeps the image file on disk: the prompt names its
// path, so the agent can read it; only the stale-paste sweep retires it.
func TestSpawnSuccessKeepsImagesForTheAgent(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	focusFormPrompt(t, m)
	m.form.name.SetValue("with-pic")
	m.form.dir.SetValue(t.TempDir())
	path := tempImage(t, "pic.png")
	pasteFormImage(t, m, path)
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)
	if len(m.sessionRows()) != 1 {
		t.Fatalf("spawn did not create a session: %v", sessionNames(m))
	}
	if !strings.Contains(m.workspace.sessions[0].LaunchPrompt, path) {
		t.Fatalf("launch prompt does not name the image: %q", m.workspace.sessions[0].LaunchPrompt)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("image file was dropped on success: %v", err)
	}
}

// A generic spawn failure (not a hint-eligible refusal) leaves the form up
// with its images restored, so the prompt can be fixed and resubmitted.
func TestSpawnGenericErrorReturnsImagesToForm(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	focusFormPrompt(t, m)
	m.form.name.SetValue("agent")
	m.form.dir.SetValue(t.TempDir())
	path := tempImage(t, "pic.png")
	id := pasteFormImage(t, m, path)
	_, cmd := m.submitForm()
	// A generic worker failure: the store is unopenable before the effect runs.
	m.services.store.Close()
	m.applyCmd(t, cmd)
	if m.mode != modeForm {
		t.Fatalf("form should stay up; mode=%v", m.mode)
	}
	if got := m.form.prompt.attachments; len(got) != 1 || got[0].id != id {
		t.Fatalf("attachments = %+v, want the chip kept for the retry", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("image the prompt still names must survive: %v", err)
	}
}

func TestSpawnFailurePreservesNewAttachments(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	request := spawnRequest{kind: spawnForm, composerGen: m.form.prompt.gen, images: []imageAttachment{{path: "old"}}}
	m.form.prompt.attachments = []imageAttachment{{path: "new"}}
	m.returnSpawnImages(request)
	if len(m.form.prompt.attachments) != 2 {
		t.Fatal("failed spawn discarded a newly pasted attachment")
	}
}

func TestQuickSpawnCompletionPreservesEditedDraft(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.input.SetValue("first task")
	request := spawnRequest{kind: spawnQuick, composerGen: m.quick.gen}
	m.dispatchSpawn(request)
	request = m.effects.pending[0].request.(spawnRequest)
	m.quick.input.SetValue("next task")
	m.applySpawnEffect(request, spawnEffectResult{session: store.Session{ID: "child", Name: "child"}}, nil)
	if m.quick.input.Value() != "next task" {
		t.Fatal("completion cleared a newer draft")
	}
}

func TestSpawnRepeatedSubmitKeepsOneAcceptedJob(t *testing.T) {
	m := buildModel(t)
	submitFormSpawn(t, m, "first", t.TempDir())
	_, repeated := m.submitForm()
	if repeated != nil {
		m.applyTestMsg(t, repeated())
	}
	jobs := len(m.effects.pending)
	if m.effects.active != nil {
		jobs++
	}
	if jobs != 1 {
		t.Fatal("repeat submit accepted a duplicate spawn")
	}
}

func TestSpawnRetryRetainsManagerAndConversationIdentity(t *testing.T) {
	m := buildModel(t)
	tool := m.services.cfg.Tools["claude"]
	tool.Command = "am-missing-cli-xyz"
	tool.SessionIDFlag = "--session-id"
	m.services.cfg.Tools["claude"] = tool
	command := submitFormSpawn(t, m, "agent", t.TempDir())
	first := m.effects.active.request.(spawnRequest)
	msg := command().(effectCompletedMsg)
	m.applyTestMsg(t, msg)
	retry, ok := m.launchHint.fix.effectRetry.(spawnRequest)
	if !ok || retry.id != first.id || retry.plan == nil || retry.plan.AgentSessionID == "" {
		t.Fatalf("retry lost its captured identity: %+v", retry)
	}
	m.enqueueEffect(retry, 0, false)
	msg = m.nextEffectCmd()().(effectCompletedMsg)
	result := msg.result.(spawnEffectResult)
	if result.plan == nil || result.plan.AgentSessionID != retry.plan.AgentSessionID {
		t.Fatal("retry regenerated the conversation identity")
	}
}

func TestGroupCompletionPreservesEditedForm(t *testing.T) {
	m := buildModel(t)
	m.openGroupForm()
	m.groupForm.name.SetValue("first")
	m.dispatchGroup(groupRequest{path: "first", gen: m.groupForm.gen})
	request := m.effects.pending[0].request.(groupRequest)
	m.groupForm.name.SetValue("next")
	m.applyGroupEffect(request, groupEffectResult{path: "first"}, nil)
	if m.mode != modeGroupForm || m.groupForm.name.Value() != "next" {
		t.Fatal("completion closed the edited group form")
	}
	if !hasWorkspaceGroup(m, "first") {
		t.Fatal("completion lost the committed group")
	}
}
