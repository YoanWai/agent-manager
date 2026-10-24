package ui

import (
	"net/url"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const docsURL = "https://agent-manager.dev/docs/"

// noticesPanel is the messages modal: uinotices.Panel owns the list it
// builds, the cursor, the body scroll and the dismissed set, and
// uirelease.State the releases and the feed. The root keeps a refused config
// import and the notice that waits for the list to come back before it
// opens.
type noticesPanel struct {
	uinotices.Panel
	// configImportError is why the config.toml of an earlier release was
	// refused, shown as a notice until it is dismissed.
	configImportError string
	// pendingNotice is a new notice that arrived while the list was not
	// showing; flushPendingNotice opens it once the list is back.
	pendingNotice string
}

// noticeSources are the root facts the notice list is built from, copied so
// the panel never reaches into the store or the key tables itself.
func (m *Model) noticeSources() uinotices.Sources {
	sources := m.release.Sources()
	sources.GlyphMode = m.prefs.glyphMode
	sources.Store = m.services.store != nil
	sources.ConfigImportError = m.notices.configImportError
	sources.SessionKeys = m.services.keys
	sources.ListKeys = m.services.listKeys
	return sources
}

func (m *Model) activeNotices() []uinotices.Notice {
	return m.notices.Active(m.noticeSources())
}

// noticesFrame is the terminal, the release work in flight and the status
// bar as the panel's tail shows them.
func (m *Model) noticesFrame() uinotices.Frame {
	return uinotices.Frame{
		GlyphMode:  m.prefs.glyphMode,
		Width:      m.layout.width,
		Height:     m.layout.height,
		Refreshing: m.release.Refreshing,
		Applying:   m.release.Applying,
		Latest:     m.release.Latest,
		Status:     m.statusRow(),
		Theme:      noticesTheme(),
	}
}

func noticesTheme() uinotices.Theme {
	card := noticeCardHex()
	return uinotices.Theme{
		Accent:      uitheme.ColorAccent(),
		Accent2:     uitheme.ColorAccent2(),
		Bright:      uitheme.ColorBright(),
		SubtleColor: uitheme.ColorSubtle(),
		Subtle:      uitheme.SubtleStyle(),
		Muted:       uitheme.MutedStyle(),
		Value:       uitheme.ValueStyle(),
		Key:         uitheme.KeyStyle(),
		Border:      noticeBorderStyle(),
		Title:       noticeTitleStyle(),
		Fill:        func(row string, width int) string { return uitheme.Paint(row, width, card) },
	}
}

// noticeCardHex warms the messages modal's fill toward yellow.
func noticeCardHex() string { return uitheme.Mix(uitheme.PanelHex(), "#e2c044", 0.16) }

// noticeBorderStyle is the card's rounded frame, dimmed toward the same
// yellow so the outline and the fill read as one object.
func noticeBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(uitheme.Mix(uitheme.Current().Subtle, "#e2c044", 0.45)))
}

func noticeTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(uitheme.Mix(uitheme.Current().Bright, "#e2c044", 0.5))).Bold(true)
}

func bugReportURL(version string) string {
	q := url.Values{}
	q.Set("template", "bug_report.yml")
	if version != "" {
		q.Set("version", version)
	}
	return uinotices.RepoURL + "/issues/new?" + q.Encode()
}

func featureRequestURL() string {
	return uinotices.RepoURL + "/issues/new?template=feature_request.yml"
}

// startupNotice is what greets this launch; uirelease.State.Greet decides
// and records it.
func (m *Model) startupNotice() string {
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
		return
	}
	m.push(uinotices.NewSurface(noticesRoot{m}, m.notices.Panel, id))
}

// openStartupNotice greets the launch when there is something to say:
// the welcome message on the first run ever, what's new on the first run
// after an update. A dev build is the developer's own tree, not an
// install, so it never greets and never advances the stored version.
func (m *Model) openStartupNotice() {
	if m.release.Version == "dev" {
		return
	}
	if id := m.startupNotice(); id != "" {
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
}

func (m *Model) applyNotices(apply func()) {
	before := map[string]bool{}
	for _, n := range m.activeNotices() {
		before[n.ID] = true
	}
	m.keepNoticeSelection(apply)
	var added string
	for _, n := range m.activeNotices() {
		if !before[n.ID] {
			added = n.ID
			break
		}
	}
	if added == "" {
		return
	}
	if _, open := surfaceOf[*uinotices.Surface](m); open {
		return
	}
	if m.listReadyForNotice() {
		m.openNotices(added)
		return
	}
	m.notices.pendingNotice = added
}

func (m *Model) listReadyForNotice() bool {
	return !m.effects.Quitting && m.base == baseList && len(m.surfaces) == 0 && !m.rail.Busy()
}

func (m *Model) flushPendingNotice() {
	if m.notices.pendingNotice == "" || !m.listReadyForNotice() {
		return
	}
	id := m.notices.pendingNotice
	m.notices.pendingNotice = ""
	for _, n := range m.activeNotices() {
		if n.ID == id {
			m.openNotices(id)
			return
		}
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

// noticeDismissQueued reports whether the lane already carries a dismissal
// of id, running or waiting.
func (m *Model) noticeDismissQueued(id string) bool {
	queued := func(job *uieffects.Job) bool {
		if job == nil {
			return false
		}
		request, ok := job.Request.(uinotices.DismissRequest)
		return ok && request.ID == id
	}
	if queued(m.effects.Main.Active) {
		return true
	}
	for _, job := range m.effects.Main.Pending {
		if queued(job) {
			return true
		}
	}
	return false
}

// applyNoticeDismiss restores a notice whose optimistic dismissal did not
// persist, and reopens the card only when the user has not moved on since.
func (m *Model) applyNoticeDismiss(job *uieffects.Job, err error) {
	if err == nil {
		return
	}
	request := job.Request.(uinotices.DismissRequest)
	m.notices.Restore(request.ID)
	m.reportErr(err.Error())
	if request.Modal && !m.effects.Quitting && request.ForegroundGen == m.gens.foreground && (m.noticesOpen() || m.listClear()) {
		m.openNotices(request.ID)
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
