// Package release is the runtime behind the messages panel: the release
// catalog and the editorial feed it checks on a timer, the what's-new
// greeting it records in settings, and the self-update it starts. The panel
// in package notices paints what State holds; the root routes the messages
// here and answers Root.
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/ui/notices"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	"github.com/YoanWai/agent-manager/internal/update"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	DismissedSetting    = "dismissed_notices"
	LastSeenSetting     = "last_seen_version"
	WhatsNewSetting     = "whats_new_version"
	WhatsNewFromSetting = "whats_new_from_version"

	tickInterval = 10 * time.Minute
)

// State is this build's release tag plus a newer release found on GitHub,
// so the header can badge it, and the feed folded in next to the built-in
// notices. Applying marks an in-flight self-update; RestartPath, once set,
// tells main to exec the freshly swapped binary after the program exits.
type State struct {
	Version        string
	Latest         string
	URL            string
	Releases       []update.Release
	Available      notices.ReleaseRange
	Installed      notices.ReleaseRange
	Checked        bool
	Refreshing     bool
	RefreshPending int
	// FeedPending holds until the launch's own feed fetch lands; the fetch
	// writes its cache before the UI applies its result.
	FeedPending bool
	Applying    bool
	RestartPath string
	// WhatsNewVersion mirrors the persisted whats_new_version setting so
	// the notices list, rebuilt every frame, never reads the database.
	WhatsNewVersion string
	WhatsNewFrom    string
	Feed            []feed.Message
}

type Store interface {
	Setting(key string) (string, error)
	SetSetting(key, value string) error
}

type Root interface {
	ApplyNotices(apply func())
	KeepNoticeSelection(apply func())
	ReportErr(text string)
	ReportDone(text string)
	// Quit quits so main can exec RestartPath.
	Quit() tea.Cmd
}

// New restores the what's-new versions that are still current. A read
// failure just means no notice.
func New(st Store, version string) State {
	return State{Version: version, FeedPending: true, WhatsNewVersion: setting(st, WhatsNewSetting), WhatsNewFrom: setting(st, WhatsNewFromSetting)}
}

func setting(st Store, key string) string {
	value, err := st.Setting(key)
	if err != nil {
		return ""
	}
	return value
}

// UseCached paints the catalog already on disk, so post-update summaries
// show before the first check returns.
func (s *State) UseCached(configDir string) {
	cached := update.Cached(configDir, s.Version)
	s.Latest = cached.Latest
	s.URL = cached.URL
	s.Releases = cached.Releases
	s.Checked = len(cached.Releases) > 0
}

func Dismissed(st Store) map[string]bool {
	dismissed := map[string]bool{}
	raw, err := st.Setting(DismissedSetting)
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

const SetupGreeting = "setup"

// Greet decides what greets this launch: the first-run setup the first time
// the manager ever runs, the what's-new notice the first run after an
// update, nothing otherwise. It advances the stored version as a side
// effect, so each greeting fires exactly once; the notice itself stays
// listed until dismissed.
func (s *State) Greet(st Store) (string, error) {
	seen, err := st.Setting(LastSeenSetting)
	if err != nil {
		return "", nil
	}
	if seen == s.Version {
		return "", nil
	}
	if seen == "" {
		if err := st.SetSetting(LastSeenSetting, s.Version); err != nil {
			return "", err
		}
		return SetupGreeting, nil
	}
	if !update.Newer(s.Version, seen) {
		return "", st.SetSetting(LastSeenSetting, s.Version)
	}
	for _, write := range [][2]string{{WhatsNewFromSetting, seen}, {WhatsNewSetting, s.Version}, {LastSeenSetting, s.Version}} {
		if err := st.SetSetting(write[0], write[1]); err != nil {
			return "", err
		}
	}
	s.WhatsNewFrom = seen
	s.WhatsNewVersion = s.Version
	s.IndexRanges()
	return "whatsnew-" + s.Version, nil
}

func (s *State) Sources() notices.Sources {
	messages := make([]notices.Message, len(s.Feed))
	for i, msg := range s.Feed {
		messages[i] = notices.Message{ID: msg.ID, Title: msg.Title, Headline: msg.Headline, Body: msg.Body, Accent: msg.Accent, URL: msg.URL}
	}
	return notices.Sources{
		Update: notices.Update{
			Version:    s.Version,
			Latest:     s.Latest,
			URL:        s.URL,
			Available:  s.Available,
			Installed:  s.Installed,
			Checked:    s.Checked,
			Refreshing: s.Refreshing,
			Applying:   s.Applying,
		},
		Feed:            messages,
		WhatsNewVersion: s.WhatsNewVersion,
		WhatsNewFrom:    s.WhatsNewFrom,
	}
}

// IndexRanges keeps catalog scans out of the paint path. It runs only when
// startup state or a background GitHub result changes.
func (s *State) IndexRanges() {
	s.Available = releaseRange(update.Between(s.Releases, s.Version, s.Latest))
	s.Installed = releaseRange(update.Between(s.Releases, s.WhatsNewFrom, s.Version))
}

func releaseRange(catalog update.ReleaseRange) notices.ReleaseRange {
	out := notices.ReleaseRange{Complete: catalog.Complete}
	for _, release := range catalog.Releases {
		changes := make([]notices.Change, len(release.Changes))
		for i, change := range release.Changes {
			changes[i] = notices.Change{Kind: change.Kind, Scope: change.Scope, Text: change.Text, Author: change.Author}
		}
		highlights := make([]notices.Highlight, len(release.Highlights))
		for i, highlight := range release.Highlights {
			highlights[i] = notices.Highlight{Label: highlight.Label, Text: highlight.Text}
		}
		thanks := make([]notices.Thanks, len(release.Thanks))
		for i, entry := range release.Thanks {
			thanks[i] = notices.Thanks{Handle: entry.Handle, Group: entry.Group, Text: entry.Text, Refs: entry.Refs}
		}
		out.Releases = append(out.Releases, notices.Release{
			Version:      release.Version,
			URL:          release.URL,
			PublishedAt:  release.PublishedAt,
			Headline:     release.Headline,
			Summary:      release.Summary,
			Highlights:   highlights,
			Thanks:       thanks,
			Changes:      changes,
			TotalChanges: release.TotalChanges,
		})
	}
	return out
}

// UpdateMsg carries the result of a GitHub release check. A failed check may
// still carry a stale catalog; an empty failure leaves the screen unchanged.
type UpdateMsg struct {
	Latest   string
	URL      string
	Releases []update.Release
	Failed   bool
	Manual   bool
	Err      error
}

type FeedMsg struct {
	Messages []feed.Message
	Failed   bool
	Manual   bool
	Startup  bool
	Err      error
}

// AppliedMsg reports the self-update download-and-swap: on success Path is
// the binary to restart into.
type AppliedMsg struct {
	Path     string
	Result   update.Result
	UpToDate bool
	Err      error
}

type TickMsg struct{}

// Tick re-runs the checks while the manager stays open for days at a time.
// Both are cache-backed, so most ticks cost one disk read.
func Tick() tea.Cmd {
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return TickMsg{} })
}

// Check hits GitHub Releases (throttled by update.Check via an on-disk
// cache) off the event loop. Any failure resolves to a failed message so the
// TUI simply shows no badge.
func (s *State) Check() tea.Cmd {
	version := s.Version
	return func() tea.Msg { return fetchUpdates(version, false) }
}

func (s *State) CheckFeed() tea.Cmd {
	version := s.Version
	return func() tea.Msg { return fetchFeed(version, false) }
}

func (s *State) CheckStartupFeed() tea.Cmd {
	version := s.Version
	return func() tea.Msg {
		msg := fetchFeed(version, false)
		msg.Startup = true
		return msg
	}
}

// Refresh is one manual pass over releases and the feed, refused while the
// previous one is still out.
func (s *State) Refresh() tea.Cmd {
	if s.Refreshing {
		return nil
	}
	s.Refreshing = true
	s.RefreshPending = 2
	version := s.Version
	return tea.Batch(
		func() tea.Msg { return fetchUpdates(version, true) },
		func() tea.Msg { return fetchFeed(version, true) },
	)
}

func (s *State) finishRefresh() {
	if s.RefreshPending > 0 {
		s.RefreshPending--
	}
	s.Refreshing = s.RefreshPending > 0
}

func fetchUpdates(version string, force bool) tea.Msg {
	dir, err := config.Dir()
	if err != nil {
		return UpdateMsg{Failed: true, Manual: force, Err: err}
	}
	var result update.Result
	if force {
		result, err = update.Refresh(context.Background(), dir, version)
	} else {
		result, err = update.Check(context.Background(), dir, version)
	}
	if err != nil {
		return UpdateMsg{
			Latest: result.Latest, URL: result.URL, Releases: result.Releases,
			Failed: true, Manual: force, Err: err,
		}
	}
	return UpdateMsg{Latest: result.Latest, URL: result.URL, Releases: result.Releases, Manual: force}
}

func fetchFeed(version string, force bool) FeedMsg {
	dir, err := config.Dir()
	if err != nil {
		return FeedMsg{Failed: true, Manual: force, Err: err}
	}
	var messages []feed.Message
	if force {
		messages, err = feed.Refresh(context.Background(), dir, version)
	} else {
		messages, err = feed.Fetch(context.Background(), dir, version)
	}
	if err != nil {
		return FeedMsg{Messages: messages, Failed: true, Manual: force, Err: err}
	}
	return FeedMsg{Messages: messages, Manual: force}
}

func (s *State) Route(root Root, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case UpdateMsg:
		if msg.Manual {
			s.finishRefresh()
		}
		if msg.Failed && len(msg.Releases) == 0 {
			if msg.Manual && msg.Err != nil {
				root.ReportErr("refresh failed: " + msg.Err.Error())
			}
			return nil, true
		}
		root.ApplyNotices(func() {
			s.Latest = msg.Latest
			s.URL = msg.URL
			s.Releases = msg.Releases
			s.Checked = true
			s.IndexRanges()
		})
		if msg.Manual && msg.Err != nil {
			root.ReportErr("refresh failed: " + msg.Err.Error())
		}
		return nil, true

	case AppliedMsg:
		s.Applying = false
		if len(msg.Result.Releases) > 0 {
			root.KeepNoticeSelection(func() {
				s.Latest = msg.Result.Latest
				s.URL = msg.Result.URL
				s.Releases = msg.Result.Releases
				s.Checked = true
				s.IndexRanges()
			})
		}
		if msg.Err != nil {
			root.ReportErr("update failed: " + msg.Err.Error())
			return nil, true
		}
		if msg.UpToDate {
			root.KeepNoticeSelection(func() {
				s.Latest = ""
				s.URL = ""
				if len(msg.Result.Releases) == 0 {
					s.Releases = nil
					s.Checked = true
				}
				s.IndexRanges()
			})
			root.ReportDone("already up to date")
			return nil, true
		}
		s.RestartPath = msg.Path
		return root.Quit(), true

	case TickMsg:
		return tea.Batch(s.Check(), s.CheckFeed(), Tick()), true

	case FeedMsg:
		if msg.Startup {
			s.FeedPending = false
		}
		if msg.Manual {
			s.finishRefresh()
		}
		if !msg.Failed || len(msg.Messages) > 0 {
			root.ApplyNotices(func() { s.Feed = msg.Messages })
		}
		if msg.Manual && msg.Err != nil {
			root.ReportErr("refresh failed: " + msg.Err.Error())
		}
		return nil, true
	}
	return nil, false
}

// ApplyUpdate is the self-update seam: tests swap it for a fake instead of
// downloading a release.
var ApplyUpdate = update.Apply

// RefreshForApply makes u resolve the newest release at action time, even
// when several releases landed after the notice was first cached.
var RefreshForApply = update.Refresh

// DetectManager is the install-source seam: tests swap it to steer u
// between the in-place swap and a delegated package manager.
var DetectManager = update.DetectManager

// Apply routes u by install source. A package-manager install hands the
// terminal to that manager's upgrade command; a direct install downloads the
// latest release off the event loop and swaps the running binary. Both
// report the path to restart into.
func (s *State) Apply() tea.Cmd {
	execPath, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return AppliedMsg{Err: err} }
	}
	manager := DetectManager(execPath)
	if manager.Advice != "" {
		return func() tea.Msg { return AppliedMsg{Err: errors.New(manager.Advice)} }
	}
	if manager.Delegated() {
		return delegatedCmd(manager, execPath)
	}
	version := s.Version
	return func() tea.Msg {
		dir, err := config.Dir()
		if err != nil {
			return AppliedMsg{Err: err}
		}
		result, err := RefreshForApply(context.Background(), dir, version)
		if err != nil {
			return AppliedMsg{Result: result, Err: err}
		}
		if result.Latest == "" {
			return AppliedMsg{Result: result, UpToDate: true}
		}
		if err := ApplyUpdate(context.Background(), result.Latest, execPath); err != nil {
			return AppliedMsg{Result: result, Err: err}
		}
		return AppliedMsg{Path: execPath, Result: result}
	}
}

// delegatedCmd suspends the TUI and runs the manager's upgrade command on
// the real terminal, so its progress output and any password prompt work as
// in a plain shell.
func delegatedCmd(manager update.Manager, execPath string) tea.Cmd {
	command := exec.Command(manager.Command[0], manager.Command[1:]...)
	return uiterminal.ExecProcess(command, func(err error) tea.Msg {
		return delegatedResult(manager, execPath, err)
	})
}

func delegatedResult(manager update.Manager, execPath string, err error) AppliedMsg {
	if err != nil {
		return AppliedMsg{Err: fmt.Errorf("%s: %w", manager.String(), err)}
	}
	return AppliedMsg{Path: update.RestartTarget(execPath)}
}
