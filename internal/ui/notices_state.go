package ui

import (
	"github.com/YoanWai/agent-manager/internal/feed"
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
