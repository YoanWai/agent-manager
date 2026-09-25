package ui

import (
	"context"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/update"
	tea "github.com/charmbracelet/bubbletea"
)

// updateInfo is this build's release tag plus a newer release found on
// GitHub, so the header can badge it. applying marks an in-flight
// self-update; restartPath, once set, tells main to exec the freshly
// swapped binary after the program exits.
type updateInfo struct {
	version        string
	latest         string
	url            string
	releases       []update.Release
	available      update.ReleaseRange
	installed      update.ReleaseRange
	checked        bool
	refreshing     bool
	refreshPending int
	applying       bool
	restartPath    string
}

// updateAppliedMsg reports the self-update download-and-swap: on success
// path is the binary to restart into.
type updateAppliedMsg struct {
	path     string
	result   update.Result
	upToDate bool
	err      error
}

// updateMsg carries the result of a GitHub release check. A failed check may
// still carry a stale catalog; an empty failure leaves the screen unchanged.
type updateMsg struct {
	latest   string
	url      string
	releases []update.Release
	failed   bool
	manual   bool
	err      error
}

// feedMsg carries the editorial message feed and any refresh failure.
type feedMsg struct {
	messages []feed.Message
	failed   bool
	manual   bool
	err      error
}

type updateTickMsg struct{}

// updateTick re-runs the release check while the manager stays open for
// days at a time. update.Check serves most ticks from its on-disk cache,
// so this only reaches GitHub once the cache goes stale.
func (m *Model) updateTick() tea.Cmd {
	return tea.Tick(updateTickInterval, func(time.Time) tea.Msg { return updateTickMsg{} })
}

// checkForUpdate hits GitHub Releases (throttled by update.Check via an
// on-disk cache) off the event loop and reports a newer release. Any
// failure resolves to a failed message so the TUI simply shows no badge.
func (m *Model) checkForUpdate() tea.Msg {
	return m.fetchUpdates(false)
}

func (m *Model) refreshUpdates() tea.Msg {
	return m.fetchUpdates(true)
}

func (m *Model) fetchUpdates(force bool) tea.Msg {
	dir, err := config.Dir()
	if err != nil {
		return updateMsg{failed: true, manual: force, err: err}
	}
	var result update.Result
	if force {
		result, err = update.Refresh(context.Background(), dir, m.update.version)
	} else {
		result, err = update.Check(context.Background(), dir, m.update.version)
	}
	if err != nil {
		return updateMsg{
			latest: result.Latest, url: result.URL, releases: result.Releases,
			failed: true, manual: force, err: err,
		}
	}
	return updateMsg{latest: result.Latest, url: result.URL, releases: result.Releases, manual: force}
}

// checkFeed pulls the remote message feed off the event loop. Like the
// release check it is cache-backed, so most ticks cost one disk read.
func (m *Model) checkFeed() tea.Msg {
	return m.fetchFeed(false)
}

func (m *Model) refreshFeed() tea.Msg {
	return m.fetchFeed(true)
}

func (m *Model) fetchFeed(force bool) tea.Msg {
	dir, err := config.Dir()
	if err != nil {
		return feedMsg{failed: true, manual: force, err: err}
	}
	var messages []feed.Message
	if force {
		messages, err = feed.Refresh(context.Background(), dir, m.update.version)
	} else {
		messages, err = feed.Fetch(context.Background(), dir, m.update.version)
	}
	if err != nil {
		return feedMsg{messages: messages, failed: true, manual: force, err: err}
	}
	return feedMsg{messages: messages, manual: force}
}
