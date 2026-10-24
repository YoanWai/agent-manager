package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/feed"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	"github.com/charmbracelet/x/ansi"
)

func TestUpdateMsgSetsAndClearsBadge(t *testing.T) {
	m := &Model{
		layout: layoutState{width: 120},
	}
	m.Update(updateMsg{latest: "v0.11.1", url: "https://example/rel"})
	if m.update.latest != "v0.11.1" || m.update.url != "https://example/rel" {
		t.Errorf("badge not set: %q %q", m.update.latest, m.update.url)
	}
	m.Update(updateMsg{})
	if m.update.latest != "" || m.update.url != "" {
		t.Errorf("an up-to-date result should clear the badge: %q %q", m.update.latest, m.update.url)
	}
}

func TestFailedUpdateCheckKeepsBadge(t *testing.T) {
	m := &Model{
		layout: layoutState{width: 120},
		update: updateInfo{latest: "v0.11.1", url: "https://example/rel"},
	}
	m.Update(updateMsg{failed: true})
	if m.update.latest != "v0.11.1" || m.update.url != "https://example/rel" {
		t.Errorf("a failed check must leave the badge alone: %q %q", m.update.latest, m.update.url)
	}
}

func TestFeedResultClearsThePendingAlert(t *testing.T) {
	cases := []struct {
		name string
		msg  feedMsg
		open bool
	}{
		{name: "new entry", msg: feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}}, open: true},
		{name: "nothing new", msg: feedMsg{}, open: false},
		{name: "refused", msg: feedMsg{failed: true, err: errors.New("feed down")}, open: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := footModel(t)
			m.mode = modeList
			m.notices.feedPending = true
			tc.msg.startup = true
			m.Update(tc.msg)
			if m.notices.feedPending {
				t.Fatal("the feed result left the check pending")
			}
			if _, label := m.messagesAlert(); label == uinotices.FeedPendingText {
				t.Fatal("the checking alert stayed up")
			}
			if opened := m.mode == modeNotices; opened != tc.open {
				t.Fatalf("panel open = %v, want %v", opened, tc.open)
			}
		})
	}
}

func TestFeedPendingWaitsForStartupResult(t *testing.T) {
	for _, tc := range []struct {
		name   string
		manual bool
	}{
		{name: "manual", manual: true},
		{name: "periodic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := footModel(t)
			m.notices.feedPending = true
			m.Update(feedMsg{manual: tc.manual})
			if !m.notices.feedPending {
				t.Fatal("a non-startup result cleared the pending startup check")
			}
			m.Update(feedMsg{startup: true})
			if m.notices.feedPending {
				t.Fatal("the startup result left the check pending")
			}
		})
	}
}

func TestCheckingAlertOpensMessagesByClick(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.notices.feedPending = true
	view := ansi.Strip(preparedView(m))
	if !strings.Contains(view, uinotices.FeedPendingText) {
		t.Fatalf("footer should say the feed check has not landed:\n%s", view)
	}
	rowText := ""
	x, y := 0, 0
	for row, line := range strings.Split(view, "\n") {
		col := strings.Index(line, uinotices.FeedPendingText)
		if col < 0 {
			continue
		}
		rowText = line
		x, y = ansi.StringWidth(line[:col]), row
		break
	}
	if rowText == "" {
		t.Fatal("the checking alert was not on its own row")
	}
	if m = leftClick(m, x, y); m.mode != modeNotices {
		t.Fatalf("a click on the checking alert should open messages, mode=%v", m.mode)
	}
	m.handleNoticesKey(key("esc"))
	if m.mode != modeList {
		t.Fatal("esc should close messages")
	}
	m.handleKey(key("M"))
	if m.mode != modeNotices {
		t.Fatalf("M should open messages while the check is pending, mode=%v", m.mode)
	}
}

func TestOpenPanelSaysTheFeedCheckHasNotLanded(t *testing.T) {
	m := modalModel(t)
	m.notices.feedPending = true
	got := ansi.Strip(preparedView(m))
	if !strings.Contains(got, uinotices.FeedPendingText) {
		t.Fatalf("an open panel should say the feed check has not landed:\n%s", got)
	}
}

func TestUpdateTickReArms(t *testing.T) {
	m := &Model{
		layout: layoutState{width: 120},
	}
	if _, cmd := m.Update(updateTickMsg{}); cmd == nil {
		t.Error("update tick should re-arm the timer and re-check")
	}
}
