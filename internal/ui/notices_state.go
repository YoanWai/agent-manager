package ui

import (
	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

type noticesState struct {
	noticeHit noticeHit
	// dismissed holds the notice ids the user closed for good; the set
	// persists in settings so a dismissed message never comes back.
	dismissed    map[string]bool
	noticeCursor int
	noticeScroll int
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

// noticesPanel is the messages modal. It owns the list it builds, the
// cursor, the body scroll and the legend hit; the release checks, the
// dismissal write and the decision to open it over the list stay in root.
type noticesPanel struct{ noticesState }

// noticesHost is what the panel needs from the root. The release refresh,
// the self-update and the dismissal write are root effects its keys start.
type noticesHost interface {
	size() (width, height int)
	currentMode() mode
	setMode(mode)
	noticeSources() noticeSources
	refreshNotices() tea.Cmd
	startUpdate() tea.Cmd
	dismissNotice(id string)
	statusRow() string
}

// noticeSources are the root facts the notice list is built from, copied so
// the panel never reaches into the store or the key tables itself.
type noticeSources struct {
	store       bool
	update      updateInfo
	sessionKeys keybind.Table
	listKeys    keybind.Table
}

func (m *Model) size() (width, height int) { return m.layout.width, m.layout.height }

func (m *Model) currentMode() mode { return m.mode }

func (m *Model) setMode(next mode) { m.mode = next }

func (m *Model) noticeSources() noticeSources {
	return noticeSources{
		store:       m.services.store != nil,
		update:      m.update,
		sessionKeys: m.services.keys,
		listKeys:    m.services.listKeys,
	}
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
	return m.statusMessage("✕", "●", "▲")
}
