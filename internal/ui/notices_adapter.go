package ui

import (
	"encoding/json"
	"net/url"

	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/store"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	"github.com/YoanWai/agent-manager/internal/update"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	dismissedNoticesSetting = "dismissed_notices"
	lastSeenVersionSetting  = "last_seen_version"
	whatsNewVersionSetting  = "whats_new_version"
	whatsNewFromSetting     = "whats_new_from_version"

	repoURL = uinotices.RepoURL
	docsURL = "https://agent-manager.dev/docs/"
)

// noticesPanel is the messages modal: uinotices.Panel owns the list it
// builds, the cursor, the body scroll and the dismissed set. The root keeps
// what the list is built from (the persisted what's-new versions, a refused
// config import, the fetched feed), and the notice that waits
// for the list to come back before it opens.
type noticesPanel struct {
	uinotices.Panel
	// whatsNewVersion mirrors the persisted whats_new_version setting so
	// the notices list, rebuilt every frame, never reads the database.
	whatsNewVersion     string
	whatsNewFromVersion string
	// configImportError is why the config.toml of an earlier release was
	// refused, shown as a notice until it is dismissed.
	configImportError string
	// feedMessages is the remote message feed, refreshed on the update
	// tick and folded into the notices next to the built-in ones.
	feedMessages []feed.Message
	// pendingNotice is a new notice that arrived while the list was not
	// showing; flushPendingNotice opens it once the list is back.
	pendingNotice string
}

// noticeSources are the root facts the notice list is built from, copied so
// the panel never reaches into the store or the key tables itself.
func (m *Model) noticeSources() uinotices.Sources {
	messages := make([]uinotices.Message, len(m.notices.feedMessages))
	for i, msg := range m.notices.feedMessages {
		messages[i] = uinotices.Message{ID: msg.ID, Title: msg.Title, Headline: msg.Headline, Body: msg.Body, Accent: msg.Accent, URL: msg.URL}
	}
	return uinotices.Sources{
		GlyphMode: m.prefs.glyphMode,
		Store:     m.services.store != nil,
		Update: uinotices.Update{
			Version:    m.update.version,
			Latest:     m.update.latest,
			URL:        m.update.url,
			Available:  m.update.available,
			Installed:  m.update.installed,
			Checked:    m.update.checked,
			Refreshing: m.update.refreshing,
			Applying:   m.update.applying,
		},
		Feed:              messages,
		WhatsNewVersion:   m.notices.whatsNewVersion,
		WhatsNewFrom:      m.notices.whatsNewFromVersion,
		ConfigImportError: m.notices.configImportError,
		SessionKeys:       m.services.keys,
		ListKeys:          m.services.listKeys,
	}
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
		Refreshing: m.update.refreshing,
		Applying:   m.update.applying,
		Latest:     m.update.latest,
		Status:     m.statusRow(),
		Theme:      noticesTheme(),
	}
}

func noticesTheme() uinotices.Theme {
	card := noticeCardHex()
	return uinotices.Theme{
		Accent:      colorAccent(),
		Accent2:     colorAccent2(),
		Bright:      colorBright(),
		SubtleColor: colorSubtle(),
		Subtle:      subtleStyle(),
		Muted:       mutedStyle(),
		Value:       valueStyle(),
		Key:         keyStyle(),
		Border:      noticeBorderStyle(),
		Title:       noticeTitleStyle(),
		Fill:        func(row string, width int) string { return paint(row, width, card) },
	}
}

// noticeCardHex warms the messages modal's fill toward yellow.
func noticeCardHex() string { return mix(panelHex(), "#e2c044", 0.16) }

// noticeBorderStyle is the card's rounded frame, dimmed toward the same
// yellow so the outline and the fill read as one object.
func noticeBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current().Subtle, "#e2c044", 0.45)))
}

func noticeTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current().Bright, "#e2c044", 0.5))).Bold(true)
}

// indexReleaseRanges keeps catalog scans out of the paint path. It runs only
// when startup state or a background GitHub result changes.
func (m *Model) indexReleaseRanges() {
	m.update.available = noticeReleaseRange(update.Between(m.update.releases, m.update.version, m.update.latest))
	m.update.installed = noticeReleaseRange(update.Between(m.update.releases, m.notices.whatsNewFromVersion, m.update.version))
}

// noticeReleaseRange copies a catalog range into the panel's values.
func noticeReleaseRange(releaseRange update.ReleaseRange) uinotices.ReleaseRange {
	out := uinotices.ReleaseRange{Complete: releaseRange.Complete}
	for _, release := range releaseRange.Releases {
		changes := make([]uinotices.Change, len(release.Changes))
		for i, change := range release.Changes {
			changes[i] = uinotices.Change{Kind: change.Kind, Text: change.Text, Author: change.Author}
		}
		out.Releases = append(out.Releases, uinotices.Release{
			Version:      release.Version,
			URL:          release.URL,
			Headline:     release.Headline,
			Summary:      release.Summary,
			Highlights:   release.Highlights,
			Thanks:       release.Thanks,
			Changes:      changes,
			TotalChanges: release.TotalChanges,
		})
	}
	return out
}

func bugReportURL(version string) string {
	q := url.Values{}
	q.Set("template", "bug_report.yml")
	if version != "" {
		q.Set("version", version)
	}
	return repoURL + "/issues/new?" + q.Encode()
}

func featureRequestURL() string {
	return repoURL + "/issues/new?template=feature_request.yml"
}

// startupNotice decides what greets this launch: the welcome notice the
// first time the manager ever runs, the what's-new notice the first run
// after an update, nothing otherwise. It advances the stored version as a
// side effect, so each greeting fires exactly once; the notice itself
// stays listed until dismissed.
func (m *Model) startupNotice() string {
	seen, err := m.services.store.Setting(lastSeenVersionSetting)
	if err != nil {
		return ""
	}
	if seen == m.update.version {
		return ""
	}
	if seen == "" {
		if err := m.services.store.SetSetting(lastSeenVersionSetting, m.update.version); err != nil {
			m.reportErr(err.Error())
			return ""
		}
		return uinotices.Welcome
	}
	if !update.Newer(m.update.version, seen) {
		if err := m.services.store.SetSetting(lastSeenVersionSetting, m.update.version); err != nil {
			m.reportErr(err.Error())
		}
		return ""
	}
	if err := m.services.store.SetSetting(whatsNewFromSetting, seen); err != nil {
		m.reportErr(err.Error())
		return ""
	}
	if err := m.services.store.SetSetting(whatsNewVersionSetting, m.update.version); err != nil {
		m.reportErr(err.Error())
		return ""
	}
	if err := m.services.store.SetSetting(lastSeenVersionSetting, m.update.version); err != nil {
		m.reportErr(err.Error())
		return ""
	}
	m.notices.whatsNewFromVersion = seen
	m.notices.whatsNewVersion = m.update.version
	m.indexReleaseRanges()
	return "whatsnew-" + m.update.version
}

// loadWhatsNewVersion restores the version whose what's-new notice is
// still current. A read failure just means no notice.
func loadWhatsNewVersion(st *store.Store) string {
	version, err := st.Setting(whatsNewVersionSetting)
	if err != nil {
		return ""
	}
	return version
}

func loadWhatsNewFromVersion(st *store.Store) string {
	version, err := st.Setting(whatsNewFromSetting)
	if err != nil {
		return ""
	}
	return version
}

func loadDismissed(st *store.Store) map[string]bool {
	dismissed := map[string]bool{}
	raw, err := st.Setting(dismissedNoticesSetting)
	if err != nil || raw == "" {
		return dismissed
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return dismissed
	}
	for _, id := range ids {
		dismissed[id] = true
	}
	return dismissed
}

// openNotices shows the panel on the notice with id, or on the first.
func (m *Model) openNotices(id string) {
	m.notices.Open(m.activeNotices(), id)
	m.mode = modeNotices
}

// openStartupNotice greets the launch when there is something to say:
// the welcome message on the first run ever, what's new on the first run
// after an update. A dev build is the developer's own tree, not an
// install, so it never greets and never advances the stored version.
func (m *Model) openStartupNotice() {
	if m.update.version == "dev" {
		return
	}
	if id := m.startupNotice(); id != "" {
		m.openNotices(id)
	}
}

// keepNoticeSelection applies a change that may add or remove notices and,
// while the panel is open, keeps the cursor on the notice it was on.
func (m *Model) keepNoticeSelection(apply func()) {
	if m.mode != modeNotices {
		apply()
		return
	}
	selected := m.notices.Selected(m.activeNotices())
	apply()
	m.notices.Reselect(m.activeNotices(), selected)
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
	if m.mode == modeNotices {
		return
	}
	if m.listReadyForNotice() {
		m.openNotices(added)
		return
	}
	m.notices.pendingNotice = added
}

func (m *Model) listReadyForNotice() bool {
	return !m.effects.quitting && m.mode == modeList && !m.rail.Searching() && !m.quick.active && !m.layout.split.resizeMode &&
		!m.rail.Reordering() && !m.rail.MenuOpen()
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

func (m *Model) handleNoticesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.applyNoticesOutcome(m.notices.Key(msg, m.activeNotices(), m.noticesFrame()))
}

func (m *Model) handleNoticesClick(line int) (tea.Model, tea.Cmd) {
	return m.applyNoticesOutcome(m.notices.Click(line, m.activeNotices(), m.noticesFrame()))
}

func (m *Model) applyNoticesOutcome(out uinotices.Outcome) (tea.Model, tea.Cmd) {
	switch out.Action {
	case uinotices.Refresh:
		return m, m.refreshNotices()
	case uinotices.StartUpdate:
		return m, m.startUpdate()
	case uinotices.OpenLink:
		return m, openLink(out.URL)
	case uinotices.Dismiss:
		m.dismissNotice(out.ID)
		if out.Close {
			m.mode = modeList
		}
	case uinotices.Close:
		m.mode = modeList
	}
	return m, nil
}

func (m *Model) viewNotices() []string {
	return m.notices.View(m.activeNotices(), m.noticesFrame())
}

// viewNoticesCard floats the panel on the backdrop and records its body,
// the rows between the edges and a column in from each, and the keys its
// bottom edge names.
func (m *Model) viewNoticesCard() string {
	card := m.viewNotices()
	frame := m.centerOnBackdrop(card)
	rect := &m.layout.card
	rect.bodyTop, rect.bodyLeft, rect.bodyWidth = rect.top+1, rect.left+2, rect.right-rect.left-4
	m.placePainted(m.notices.Foot(m.activeNotices(), m.noticesFrame()), rect.left, rect.bottom-1)
	return frame
}

func (m *Model) dismissNotice(id string) {
	if id == "" || m.services.store == nil || m.noticeDismissQueued(id) || m.notices.Dismissed(id) {
		return
	}
	m.notices.Dismiss(id)
	m.enqueueEffect(noticeDismissRequest{
		id:            id,
		foregroundGen: m.gens.foreground,
		modal:         m.mode == modeNotices,
	}, 0, false)
}

// refreshNotices is r in the panel: one manual pass over releases and the
// feed, refused while the previous one is still out.
func (m *Model) refreshNotices() tea.Cmd {
	if m.update.refreshing {
		return nil
	}
	m.update.refreshing = true
	m.update.refreshPending = 2
	m.clearErr()
	return tea.Batch(m.refreshUpdates, m.refreshFeed)
}

// startUpdate is u on the update notice, refused while one is applying.
func (m *Model) startUpdate() tea.Cmd {
	if m.update.applying {
		return nil
	}
	m.update.applying = true
	m.clearErr()
	return m.applyUpdateCmd()
}

// statusRow is the status bar message as the panel's tail shows it, empty
// when the bar is.
func (m *Model) statusRow() string {
	if m.errBar.text == "" {
		return ""
	}
	return m.statusMessage(m.glyphs().ErroredGlyph, m.glyphs().FinishedGlyph, m.glyphs().WarningGlyph)
}
