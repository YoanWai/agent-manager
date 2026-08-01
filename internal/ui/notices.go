package ui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"runtime"
	"sort"

	"github.com/YoanWai/agent-manager/internal/store"
)

const (
	noticeWelcome   = "welcome"
	noticeBugReport = "bug-report"

	dismissedNoticesSetting = "dismissed_notices"
	lastSeenVersionSetting  = "last_seen_version"
	whatsNewVersionSetting  = "whats_new_version"

	repoURL = "https://github.com/YoanWai/agent-manager"
)

// notice is one dismissible message shown in the rail's messages panel
// and readable in full from the notices modal. url is what enter opens.
type notice struct {
	id     string
	banner string
	title  string
	body   []string
	url    string
}

func (m *Model) activeNotices() []notice {
	var notices []notice
	if m.update.latest != "" {
		notices = append(notices, notice{
			id:     "update-" + m.update.latest,
			banner: "↑ " + m.update.latest + " available",
			title:  "Update available",
			body: []string{
				"Release " + m.update.latest + " is out; this build is " + m.update.version + ".",
				"Enter opens the release page with the changelog and install notes.",
			},
			url: m.update.url,
		})
	}
	if updated, err := m.store.Setting(whatsNewVersionSetting); err == nil && updated == m.update.version {
		notices = append(notices, notice{
			id:     "whatsnew-" + m.update.version,
			banner: "✦ updated to " + m.update.version,
			title:  "What's new in " + m.update.version,
			body: []string{
				"This machine now runs " + m.update.version + ".",
				"Enter opens the release notes for everything that changed.",
			},
			url: repoURL + "/releases/tag/" + m.update.version,
		})
	}
	notices = append(notices,
		notice{
			id:     noticeWelcome,
			banner: "✳ welcome — press M",
			title:  "Welcome to agent-manager",
			body: []string{
				"Every row on the left is a live agent session: enter attaches,",
				"space sends a quick prompt, ? shows every key.",
				"Messages like this one live here; x dismisses one for good.",
			},
			url: repoURL + "#readme",
		},
		notice{
			id:     noticeBugReport,
			banner: "? report a bug",
			title:  "Found a bug?",
			body: []string{
				"Enter opens a GitHub issue prefilled with your version and OS.",
				"A pane screenshot and the steps that led there help the most.",
			},
			url: bugReportURL(m.update.version),
		},
	)

	kept := notices[:0]
	for _, n := range notices {
		if !m.dismissed[n.id] {
			kept = append(kept, n)
		}
	}
	return kept
}

func bugReportURL(version string) string {
	body := fmt.Sprintf("**Version:** %s\n**OS:** %s/%s\n\n**What happened:**\n", version, runtime.GOOS, runtime.GOARCH)
	return repoURL + "/issues/new?body=" + url.QueryEscape(body)
}

// startupNotice decides what greets this launch: the welcome notice the
// first time the manager ever runs, the what's-new notice the first run
// after an update, nothing otherwise. It advances the stored version as a
// side effect, so each greeting fires exactly once; the notice itself
// stays listed until dismissed.
func (m *Model) startupNotice() string {
	seen, err := m.store.Setting(lastSeenVersionSetting)
	if err != nil {
		return ""
	}
	if seen == m.update.version {
		return ""
	}
	if err := m.store.SetSetting(lastSeenVersionSetting, m.update.version); err != nil {
		m.errBar.text = err.Error()
		return ""
	}
	if seen == "" {
		return noticeWelcome
	}
	if err := m.store.SetSetting(whatsNewVersionSetting, m.update.version); err != nil {
		m.errBar.text = err.Error()
	}
	return "whatsnew-" + m.update.version
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

func (m *Model) dismissNotice(id string) {
	m.dismissed[id] = true
	ids := make([]string, 0, len(m.dismissed))
	for dismissedID := range m.dismissed {
		ids = append(ids, dismissedID)
	}
	sort.Strings(ids)
	raw, err := json.Marshal(ids)
	if err != nil {
		m.errBar.text = err.Error()
		return
	}
	if err := m.store.SetSetting(dismissedNoticesSetting, string(raw)); err != nil {
		m.errBar.text = err.Error()
	}
}
