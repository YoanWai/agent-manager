package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
)

const docsURL = uinotices.DocsURL

var _ columnClicker = (*uinotices.Surface)(nil)

// noticesPanel is the messages modal: uinotices.Panel owns the list it
// builds, the cursor, the body scroll, the dismissed and seen sets, and
// uirelease.State the releases and the feed. The root keeps a refused config
// import and the notice that waits for the list to come back before it
// opens.
type noticesPanel struct {
	uinotices.Panel
	uinotices.Log
}

// noticeSources are the root facts the notice list is built from, copied so
// the panel never reaches into the store or the key tables itself.
func (m *Model) noticeSources() uinotices.Sources {
	sources := m.release.Sources()
	sources.Store = m.services.store != nil
	sources.ConfigImportError = m.notices.ImportError
	return sources
}

func (m *Model) activeNotices() []uinotices.Notice {
	return m.notices.Active(m.noticeSources())
}

// noticesFrame is the terminal, the release work in flight and the status
// bar as the panel's tail shows them.
func (m *Model) noticesFrame() uinotices.Frame {
	return uinotices.Frame{
		CloseKey:    m.services.listKeys.Binding(keybind.Messages),
		GlyphMode:   m.prefs.glyphMode,
		Width:       m.layout.width,
		Height:      m.layout.height,
		Refreshing:  m.release.Refreshing,
		FeedPending: m.release.FeedPending,
		Applying:    m.release.Applying,
		Latest:      m.release.Latest,
		Status:      m.statusRow(),
		Theme:       uinotices.PanelTheme(),
	}
}

const setupGreeting = uirelease.SetupGreeting

// startupGreeting is what greets this launch; uirelease.State.Greet decides
// and records it.
func (m *Model) startupGreeting() string {
	id, err := m.release.Greet(m.services.store)
	if err != nil {
		m.reportErr(err.Error())
	}
	return id
}

// openNotices shows the panel on the notice with id, or on the first. An
// open panel moves to it instead of stacking a second.
func (m *Model) openNotices(id string) {
	if s, ok := surfaceOf[*uinotices.Surface](m); ok {
		s.Panel().Open(m.activeNotices(), id)
		m.seeShownNotice(s)
		return
	}
	m.push(uinotices.NewSurface(noticesRoot{m}, m.notices.Panel, id))
}

// openStartupGreeting greets the launch when there is something to say:
// the setup on the first run ever, what's new on the first run
// after an update. A dev build is the developer's own tree, not an
// install, so it never greets and never advances the stored version.
func (m *Model) openStartupGreeting() {
	if m.release.Version == "dev" {
		return
	}
	switch id := m.startupGreeting(); id {
	case "":
	case setupGreeting:
		m.openSetup(true)
	default:
		m.openNotices(id)
	}
}

func (m *Model) noticesOpen() bool {
	_, open := surfaceOf[*uinotices.Surface](m)
	return open
}

// keepNoticeSelection applies a change that may add or remove notices and,
// while the panel is open, keeps the cursor on the notice it was on.
func (m *Model) keepNoticeSelection(apply func()) {
	s, ok := surfaceOf[*uinotices.Surface](m)
	if !ok {
		apply()
		return
	}
	selected := s.Panel().Selected(m.activeNotices())
	apply()
	s.Panel().Reselect(m.activeNotices(), selected)
	m.seeShownNotice(s)
}

func (m *Model) applyNotices(apply func()) {
	before := uinotices.IDs(m.activeNotices())
	m.keepNoticeSelection(apply)
	_, open := surfaceOf[*uinotices.Surface](m)
	staged := m.notices.Staged(uinotices.Added(before, m.activeNotices()), open, m.listReadyForNotice())
	if staged.Open != "" {
		m.openNotices(staged.Open)
		return
	}
	m.notices.Held = staged.Hold
}

func (m *Model) listReadyForNotice() bool {
	return !m.effects.Quitting && m.base == baseList && len(m.surfaces) == 0 && !m.rail.Busy()
}

func (m *Model) flushPendingNotice() {
	if m.notices.Held == "" || !m.listReadyForNotice() {
		return
	}
	id := m.notices.Take()
	if uinotices.Holds(m.activeNotices(), id) {
		m.openNotices(id)
	}
}

// paintNoticesCard floats the panel on the backdrop and records its body,
// the rows between the edges and a column in from each, and the keys its
// bottom edge names.
func (m *Model) paintNoticesCard(card []string, foot []presentation.PaintedKey) string {
	frame := m.centerOnBackdrop(card)
	rect := &m.layout.card
	rect.bodyTop, rect.bodyLeft, rect.bodyWidth = rect.top+1, rect.left+2, rect.right-rect.left-4
	m.placePainted(foot, rect.left, rect.bottom-1)
	return frame
}

func (m *Model) takeForeground() { m.gens.foreground++ }

func (m *Model) dismissNotice(id string) {
	if id == "" || m.services.store == nil || m.noticeDismissQueued(id) || m.notices.Dismissed(id) {
		return
	}
	m.notices.Dismiss(id)
	m.enqueueEffect(uinotices.DismissRequest{
		ID:            id,
		ForegroundGen: m.gens.foreground,
		Modal:         m.noticesOpen(),
	}, 0, false)
}

// seeNotice marks the notice the open panel shows as read and persists it.
func (m *Model) seeNotice(id string) {
	if id == "" || m.services.store == nil || m.notices.Seen(id) {
		return
	}
	m.notices.See(id)
	m.enqueueEffect(uinotices.SeenRequest{ID: id}, 0, false)
}

// seeShownNotice marks read the notice a cursor the root moved now shows.
func (m *Model) seeShownNotice(s *uinotices.Surface) {
	m.seeNotice(s.Panel().Selected(m.activeNotices()))
}

// noticeDismissQueued reports whether the lane already carries a dismissal
// of id, running or waiting.
func (m *Model) noticeDismissQueued(id string) bool {
	var requests []uinotices.DismissRequest
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if request, ok := job.Request.(uinotices.DismissRequest); ok {
			requests = append(requests, request)
		}
	}
	return uinotices.Queued(requests, id)
}

// applyNoticeDismiss restores a notice whose optimistic dismissal did not
// persist, and reopens the card only when the user has not moved on since.
func (m *Model) applyNoticeDismiss(job *uieffects.Job, err error) {
	if err == nil {
		return
	}
	failed := uinotices.FailedDismissal(job.Request.(uinotices.DismissRequest), err, m.effects.Quitting, m.gens.foreground, m.noticesOpen(), m.listClear())
	m.notices.Restore(failed.Restore)
	m.reportErr(failed.Err.Error())
	if failed.Reopen != "" {
		m.openNotices(failed.Reopen)
	}
}

// noticesServices is what the notices effect lane writes through.
func (m *Model) noticesServices() uinotices.Services {
	if m.services.store == nil {
		return uinotices.Services{}
	}
	return uinotices.Services{Store: m.services.store}
}

// refreshNotices is r in the panel: one manual pass over releases and the
// feed, refused while the previous one is still out.
func (m *Model) refreshNotices() tea.Cmd {
	cmd := m.release.Refresh()
	if cmd != nil {
		m.clearErr()
	}
	return cmd
}

// startUpdate is u on the update notice, refused while one is applying.
func (m *Model) startUpdate() tea.Cmd {
	if m.release.Applying {
		return nil
	}
	m.release.Applying = true
	m.clearErr()
	return m.release.Apply()
}

// statusRow is the status bar message as the panel's tail shows it, empty
// when the bar is.
func (m *Model) statusRow() string {
	if m.errBar.text == "" {
		return ""
	}
	return m.statusMessage(m.glyphs().ErroredGlyph, m.glyphs().FinishedGlyph, m.glyphs().WarningGlyph)
}

// noticesRoot is the root as the messages modal reads it.
type noticesRoot struct{ *Model }

func (r noticesRoot) ActiveNotices() []uinotices.Notice { return r.activeNotices() }

func (r noticesRoot) NoticesFrame() uinotices.Frame { return r.noticesFrame() }

func (r noticesRoot) RefreshNotices() tea.Cmd { return r.refreshNotices() }

func (r noticesRoot) StartUpdate() tea.Cmd { return r.startUpdate() }

func (r noticesRoot) MarkNotice(mark uinotices.Mark) {
	if mark.Dismiss {
		r.dismissNotice(mark.ID)
		return
	}
	r.seeNotice(mark.ID)
}

func (r noticesRoot) PaintNoticesCard(card []string, foot []presentation.PaintedKey) string {
	return r.paintNoticesCard(card, foot)
}

func (r noticesRoot) TakeForeground() { r.takeForeground() }

func (r noticesRoot) OpenLink(url string) tea.Cmd { return openLink(url) }

// releaseRoot is the root as a routed release, feed or self-update result
// reaches it.
type releaseRoot struct{ *Model }

func (r releaseRoot) ApplyNotices(apply func()) { r.applyNotices(apply) }

func (r releaseRoot) KeepNoticeSelection(apply func()) { r.keepNoticeSelection(apply) }

func (r releaseRoot) Quit() tea.Cmd {
	_, cmd := r.requestQuit()
	return cmd
}

func (m *Model) routeNoticesMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if msg, ok := msg.(browserOpenMsg); ok {
		m.handleBrowserOpen(msg)
		return routed(m, nil)
	}
	if cmd, ok := m.release.Route(releaseRoot{m}, msg); ok {
		return routed(m, cmd)
	}
	return nil, nil, false
}

// RestartPath is the binary main execs into after the program exits;
// empty when no self-update happened this run.
func (m *Model) RestartPath() string { return m.release.RestartPath }
