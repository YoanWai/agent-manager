package ui

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/testfixture"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
)

func confirmYes(t *testing.T, m *Model) {
	t.Helper()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.applyCmd(t, cmd)
	m.drainEffects(t)
}

func TestFilteredKillTargetsAndEffects(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprint(all), func(t *testing.T) {
			m := buildModel(t)
			seedGroups(t, m, "work", "hidden/deep")
			for _, name := range []string{"needle-one", "needle-two", "other"} {
				createSession(t, m, name, t.TempDir(), "work")
			}
			createSession(t, m, "needle-hidden", t.TempDir(), "hidden/deep")
			inventory := append([]store.Session(nil), m.workspace.sessions...)
			if err := m.services.store.SetGroupArchived("hidden", true); err != nil {
				t.Fatal(err)
			}
			m.applyCmd(t, m.refreshCmd())
			m.rail.SetSearch("needle", false)
			m.selectGroupRow(t, "work")
			if all {
				m.killAllLive()
			} else {
				m.killSelected()
			}
			pending := pendingConfirm(m)
			if len(pending.lifecycle.Sessions) != 2 || !strings.Contains(pending.prompt.Label, "(2") {
				t.Fatalf("targets=%+v label=%q", pending.lifecycle.Sessions, pending.prompt.Label)
			}
			confirmKill(t, m)
			for _, sess := range inventory {
				want := sess.Name == "other" || sess.Name == "needle-hidden"
				if got := m.services.tmux.Exists(sess.ID); got != want {
					t.Errorf("%s live=%v want=%v", sess.Name, got, want)
				}
			}
		})
	}
}

func TestKillRootLabelAndEffects(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "root-agent", t.TempDir(), "")
	m.selectGroupRow(t, "")
	m.killSelected()
	if label := pendingConfirm(m).prompt.Label; !strings.Contains(label, "group root (1 live sessions)") {
		t.Fatalf("label=%q", label)
	}
	confirmKill(t, m)
	m.selectGroupRow(t, "")
	m.killSelected()
	if m.errBar.text != "no live sessions to kill in root" {
		t.Fatalf("notice=%q", m.errBar.text)
	}
}

func TestArchivedLiveRowsRefuseUIKill(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "archived-live", t.TempDir(), "")
	sess := m.workspace.sessions[0]
	if err := m.services.store.SetArchived(sess.ID, true); err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, sess.Name)
	for _, action := range []string{"selected", "all", "x", "X"} {
		var cmd tea.Cmd
		switch action {
		case "selected":
			_, cmd = m.killSelected()
		case "all":
			_, cmd = m.killAllLive()
		default:
			_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(action)})
		}
		m.applyCmd(t, cmd)
		m.drainEffects(t)
		if confirmOpen(m) || !m.services.tmux.Exists(sess.ID) {
			t.Errorf("%s: card open=%v live=%v", action, confirmOpen(m), m.services.tmux.Exists(sess.ID))
		}
	}
}

func TestArchivedRelaunchDoesNotOfferConfirmation(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "zone")
	_, _, stash := seedRestoreScenario(t, m, "zone")
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	for _, group := range []bool{false, true} {
		for _, action := range []string{"revive", "all", "restart"} {
			if group {
				m.selectGroupRow(t, "zone")
			} else {
				m.selectSessionRow(t, stash.Name)
			}
			switch action {
			case "revive":
				m.reviveSelected()
			case "all":
				m.reviveAllDead()
			case "restart":
				m.restartSelected()
			}
			m.drainEffects(t)
			if confirmOpen(m) || m.services.tmux.Exists(stash.ID) {
				t.Fatalf("archived group=%v action=%s: card=%v pane=%v", group, action, confirmOpen(m), m.services.tmux.Exists(stash.ID))
			}
		}
	}
}

func TestReviveParentExcludesArchivedChildren(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "parent", t.TempDir(), "")
	parent := m.sessionRows()[0]
	child := store.Session{ID: newID(), Name: "archived-child", Tool: parent.Tool, Cwd: parent.Cwd, ParentID: parent.ID, Status: status.Dead, Archived: true}
	if err := m.services.store.CreateSession(child); err != nil {
		t.Fatal(err)
	}
	if err := m.services.tmux.Kill(parent.ID); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "parent")
	_, cmd := m.reviveSelected()
	m.applyCmd(t, cmd)
	m.drainEffects(t)
	if confirmOpen(m) || !m.services.tmux.Exists(parent.ID) || m.services.tmux.Exists(child.ID) {
		t.Fatalf("parent=%v child=%v card=%v err=%s", m.services.tmux.Exists(parent.ID), m.services.tmux.Exists(child.ID), confirmOpen(m), m.errBar.text)
	}
}

func TestArchivedDeadHintUsesRestoreBinding(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "archived", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.SetArchived(sess.ID, true); err != nil {
		t.Fatal(err)
	}
	keys := m.services.listKeys.With(keybind.Restore, bindingOf(t, "U"))
	if err := m.services.store.SetKeys(keys); err != nil {
		t.Fatal(err)
	}
	if err := uilifecycle.DeadSessionError(m.services.store, sess.ID); err == nil || err.Error() != "session is archived - press U to restore" {
		t.Fatalf("hint = %v", err)
	}
}

func TestArchivedHintWithUnboundRestoreNamesRowMenu(t *testing.T) {
	keys := keybind.DefaultList().With(keybind.Restore, keybind.Binding{})
	if got := uisession.ArchivedHint(keys); got != "session is archived - choose Restore from its row menu" {
		t.Fatalf("hint = %q", got)
	}
}

func TestFilteredReviveTargetsAndEffects(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprint(all), func(t *testing.T) {
			m := buildModel(t)
			seedGroups(t, m, "work", "hidden/deep")
			for _, name := range []string{"needle-one", "needle-two", "other"} {
				createSession(t, m, name, t.TempDir(), "work")
			}
			createSession(t, m, "needle-hidden", t.TempDir(), "hidden/deep")
			inventory := append([]store.Session(nil), m.workspace.sessions...)
			for _, sess := range inventory {
				if err := m.services.tmux.Kill(sess.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.services.store.SetGroupArchived("hidden", true); err != nil {
				t.Fatal(err)
			}
			m.applyCmd(t, m.refreshCmd())
			m.rail.SetSearch("needle", false)
			m.selectGroupRow(t, "work")
			if all {
				m.reviveAllDead()
			} else {
				m.reviveSelected()
			}
			pending := pendingConfirm(m)
			if len(pending.lifecycle.Sessions) != 2 || !strings.Contains(pending.prompt.Label, "(2") {
				t.Fatalf("targets=%+v label=%q", pending.lifecycle.Sessions, pending.prompt.Label)
			}
			confirmYes(t, m)
			for _, sess := range inventory {
				want := sess.Name == "needle-one" || sess.Name == "needle-two"
				if got := m.services.tmux.Exists(sess.ID); got != want {
					t.Errorf("%s live=%v want=%v err=%s", sess.Name, got, want, m.errBar.text)
				}
			}
		})
	}
}

func TestReviveDeadChildRejectsChangedFamily(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "parent", t.TempDir(), "")
	m.selectSessionRow(t, "parent")
	child := spawnTerminal(t, m)
	if err := m.services.tmux.Kill(child.ID); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "parent")
	m.reviveSelected()
	if sessions := pendingConfirm(m).lifecycle.Sessions; len(sessions) != 1 {
		t.Fatalf("targets=%+v", sessions)
	}
	extra := store.Session{ID: newID(), Name: "extra", Tool: child.Tool, Cwd: child.Cwd, ParentID: child.ParentID, Status: status.Dead}
	if err := m.services.store.CreateSession(extra); err != nil {
		t.Fatal(err)
	}
	confirmYes(t, m)
	if !strings.Contains(m.errBar.text, "sessions changed since confirmation") {
		t.Fatalf("notice=%q", m.errBar.text)
	}
	if m.services.tmux.Exists(child.ID) || m.services.tmux.Exists(extra.ID) {
		t.Fatal("changed family revived")
	}
}

func TestReviveRootLabelAndEmptyNotice(t *testing.T) {
	m := buildModel(t)
	for _, name := range []string{"one", "two"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	for _, sess := range m.workspace.sessions {
		if err := m.services.tmux.Kill(sess.ID); err != nil {
			t.Fatal(err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "")
	m.reviveSelected()
	if want, label := "revive group root (2 dead sessions)? brings them back.", pendingConfirm(m).prompt.Label; label != want {
		t.Fatalf("label=%q", label)
	}
	confirmYes(t, m)
	m.selectGroupRow(t, "")
	_, cmd := m.reviveSelected()
	m.applyCmd(t, cmd)
	m.drainEffects(t)
	if m.errBar.text != "no dead sessions to revive in root" {
		t.Fatalf("notice=%q", m.errBar.text)
	}
}

func TestDeadSessionErrorPreservesGoneIdentity(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "deleted", t.TempDir(), "")
	sess := m.workspace.sessions[0]
	if err := m.services.store.Delete(sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := uilifecycle.DeadSessionError(m.services.store, sess.ID); !errors.Is(err, store.ErrSessionGone) {
		t.Fatalf("deleted row error=%v", err)
	}
}

func TestDeadRecoveryHintFollowsSavedBindings(t *testing.T) {
	cases := []struct {
		revive, restart []string
		want            string
	}{
		{[]string{"U", "alt+v"}, []string{"N"}, "session is dead - press U / alt+v to revive or N to restart"},
		{nil, []string{"N"}, "session is dead - press N to restart or choose Revive from its row menu"},
		{[]string{"U"}, nil, "session is dead - press U to revive"},
		{nil, nil, "session is dead - choose Revive from its row menu"},
	}
	for _, test := range cases {
		t.Run(test.want, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "dead", t.TempDir(), "")
			sess := m.workspace.sessions[0]
			keys := m.services.listKeys.With(keybind.Revive, bindingOf(t, test.revive...)).With(keybind.Restart, bindingOf(t, test.restart...))
			if err := m.services.store.SetKeys(keys); err != nil {
				t.Fatal(err)
			}
			if err := uilifecycle.DeadSessionError(m.services.store, sess.ID); err == nil || err.Error() != test.want {
				t.Fatalf("hint=%v want %q", err, test.want)
			}
		})
	}
}

func TestRestoreArchivedGroupRecoversLegacyHiddenRowsWithoutRevivingDead(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "legacy/deep")
	if err := m.services.store.SetGroupArchived("legacy", true); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	live := store.Session{ID: newID(), Name: "hidden-live", Tool: "claude", Cwd: dir, Group: "legacy/deep", Status: status.Idle}
	dead := store.Session{ID: newID(), Name: "hidden-dead", Tool: "claude", Cwd: dir, Group: "legacy/deep", Status: status.Dead}
	stash := store.Session{ID: newID(), Name: "archived", Tool: "claude", Cwd: dir, Group: "legacy/deep", Status: status.Dead, Archived: true}
	// Legacy rows bypassed active placement validation when they were created.
	for _, sess := range []store.Session{live, dead, stash} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.services.tmux.Create(live.ID, dir, m.services.cfg.CLIs[live.Tool].Command, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	if m.focusSession(live.ID) || m.focusSession(dead.ID) {
		t.Fatal("legacy hidden rows unexpectedly listed in active view")
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "legacy")
	m.restoreSelected()
	if sessions := pendingConfirm(m).lifecycle.Sessions; len(sessions) != 1 || sessions[0].ID != stash.ID {
		t.Fatalf("restore targets=%+v", sessions)
	}
	confirmYes(t, m)
	if !m.services.tmux.Exists(live.ID) || !m.services.tmux.Exists(stash.ID) || m.services.tmux.Exists(dead.ID) {
		t.Fatalf("live=%v stash=%v dead=%v err=%s", m.services.tmux.Exists(live.ID), m.services.tmux.Exists(stash.ID), m.services.tmux.Exists(dead.ID), m.errBar.text)
	}
	m.rail.SetArchived(false)
	m.applyCmd(t, m.refreshCmd())
	for _, sess := range []store.Session{live, dead, stash} {
		if !m.focusSession(sess.ID) {
			t.Fatalf("restored group still hides %s", sess.Name)
		}
	}
}

func TestNestedGroupRestoreIsVisibleAcrossPolls(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "a/b/deep", "a/sibling", "outside")
	restored := store.Session{ID: newID(), Name: "restored", Tool: "claude", Cwd: t.TempDir(), Group: "a/b", Status: status.Dead}
	live := store.Session{ID: newID(), Name: "already-live", Tool: "claude", Cwd: t.TempDir(), Group: "a/b/deep", Status: status.Idle}
	parent := store.Session{ID: newID(), Name: "parent-stash", Tool: "claude", Cwd: t.TempDir(), Group: "a", Status: status.Dead}
	sibling := store.Session{ID: newID(), Name: "sibling-stash", Tool: "claude", Cwd: t.TempDir(), Group: "a/sibling", Status: status.Dead}
	sleeper := store.Session{ID: newID(), Name: "outside-dead", Tool: "claude", Cwd: t.TempDir(), Group: "outside", Status: status.Dead}
	for _, sess := range []store.Session{restored, live, parent, sibling, sleeper} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.services.store.SetGroupArchived("a", true); err != nil {
		t.Fatal(err)
	}
	if err := m.services.tmux.Create(live.ID, live.Cwd, m.services.cfg.CLIs[live.Tool].Command, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	beforePID, err := m.services.tmux.PanePID(live.ID)
	if err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "a/b")
	stalePoll := m.refreshCmd()()
	staleAt := time.Now()
	m.restoreSelected()
	confirmYes(t, m)
	if !m.services.tmux.Exists(restored.ID) {
		t.Fatalf("restore did not launch: %s", m.errBar.text)
	}
	if pid, err := m.services.tmux.PanePID(live.ID); err != nil || pid != beforePID {
		t.Fatalf("live restore replaced pane: %d %v", pid, err)
	}
	m.applyTestMsg(t, stalePoll)
	m.rail.SetArchived(false)
	for _, sess := range []store.Session{restored, live} {
		if !m.focusSession(sess.ID) {
			t.Fatalf("restored row invisible before poll: %s", sess.Name)
		}
	}
	stale := m.ledger.lifecycle.ReconcileListing(uilifecycle.Listing{ListedAt: staleAt, ArchivedGroups: map[string]bool{"a": true, "a/b": true, "a/b/deep": true, "a/sibling": true}})
	if stale.ArchivedGroups["a"] || stale.ArchivedGroups["a/b"] || stale.ArchivedGroups["a/b/deep"] || !stale.ArchivedGroups["a/sibling"] {
		t.Fatalf("stale poll restored archive flags: %+v", stale.ArchivedGroups)
	}
	for poll := 0; poll < 3; poll++ {
		m.applyCmd(t, m.refreshCmd())
		for _, sess := range []store.Session{restored, live} {
			if !m.focusSession(sess.ID) {
				t.Fatalf("restored row invisible after poll %d: %s", poll, sess.Name)
			}
		}
	}
	m.rail.SetArchived(true)
	for _, sess := range []store.Session{parent, sibling} {
		if !m.focusSession(sess.ID) || m.services.tmux.Exists(sess.ID) {
			t.Fatalf("unrelated archived row changed: %s", sess.Name)
		}
	}
	if m.services.tmux.Exists(sleeper.ID) {
		t.Fatal("restore revived a deliberately killed active session")
	}
	m.ledger.lifecycle.ReconcileListing(uilifecycle.Listing{ListedAt: time.Now().Add(time.Second)})
	if len(m.ledger.lifecycle.GoneGroups) != 0 {
		t.Fatalf("new poll did not retire group ledger: %+v", m.ledger.lifecycle.GoneGroups)
	}
}

func TestRestoreEmptyNestedGroupClearsAncestors(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "a", "a/b")
	if err := m.services.store.SetGroupArchived("a", true); err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "a/b")
	m.restoreSelected()
	confirmYes(t, m)
	m.rail.SetArchived(false)
	if m.groupEffectivelyArchived("a/b") {
		t.Fatal("empty subgroup restore left archived ancestry")
	}
	m.selectGroupRow(t, "a/b")
}

func TestWholeGroupArchiveRestoreIncludesFilteredAndArchivedRows(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "zone")
	for _, name := range []string{"match", "hidden", "stash"} {
		createSession(t, m, name, t.TempDir(), "zone")
	}
	var rows []store.Session
	for _, sess := range m.workspace.sessions {
		if sess.Group == "zone" {
			rows = append(rows, sess)
			if sess.Name == "stash" {
				if err := m.services.store.SetArchived(sess.ID, true); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	m.applyCmd(t, m.refreshCmd())
	m.rail.SetSearch("match", false)
	m.selectGroupRow(t, "zone")
	m.archiveSelected()
	if pending := pendingConfirm(m); len(pending.lifecycle.Sessions) != 3 || !strings.Contains(pending.prompt.Label, "whole group zone (3 sessions, 1 already archived)") {
		t.Fatalf("archive scope: %s %+v", pending.prompt.Label, pending.lifecycle.Sessions)
	}
	confirmYes(t, m)
	for _, sess := range rows {
		stored, err := m.services.store.Get(sess.ID)
		if err != nil || !stored.Archived || m.services.tmux.Exists(sess.ID) {
			t.Fatalf("archive omitted row: %+v %v", stored, err)
		}
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.rail.SetSearch("match", false)
	m.selectGroupRow(t, "zone")
	m.restoreSelected()
	if pending := pendingConfirm(m); len(pending.lifecycle.Sessions) != 3 || !strings.Contains(pending.prompt.Label, "whole group zone (3 archived sessions)") {
		t.Fatalf("restore scope: %s %+v", pending.prompt.Label, pending.lifecycle.Sessions)
	}
	confirmYes(t, m)
	for _, sess := range rows {
		stored, err := m.services.store.Get(sess.ID)
		if err != nil || stored.Archived || !m.services.tmux.Exists(sess.ID) {
			t.Fatalf("restore omitted row: %+v %v", stored, err)
		}
	}
}

func TestLivePublicArchiveRestoresWithoutRelaunchBeforeKill(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "live", t.TempDir(), "")
	sess := m.workspace.sessions[0]
	if err := m.services.store.SetAgentSessionID(sess.ID, "conversation-proof"); err != nil {
		t.Fatal(err)
	}
	before, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := m.services.tmux.PanePID(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.services.lifecycle.SetArchivedForSession("", sess.ID, true, sessioncmd.Vocabulary{}); err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, sess.Name)
	m.killSelected()
	m.killAllLive()
	if confirmOpen(m) {
		t.Fatal("archived kill opened a card")
	}
	m.restoreSelected()
	confirmYes(t, m)
	after, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current, err := m.services.tmux.PanePID(sess.ID); err != nil || current != pid || after.AgentSessionID != before.AgentSessionID || !after.AgentLaunchedAt.Equal(before.AgentLaunchedAt) {
		t.Fatalf("restore relaunched archived pane: %+v PID=%d %v", after, current, err)
	}
	m.rail.SetArchived(false)
	m.focusSession(sess.ID)
	m.killSelected()
	confirmYes(t, m)
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("active Kill did not stop restored pane")
	}
	if stored, err := m.services.store.Get(sess.ID); err != nil || stored.Archived || stored.Status != status.Dead {
		t.Fatalf("Kill removed/restored wrong state: %+v %v", stored, err)
	}
}

func TestGroupRestoreRetryKeepsScopeAndRollsBackFailedLaunches(t *testing.T) {
	for _, failFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("first=%v", failFirst), func(t *testing.T) {
			m := buildModel(t)
			seedGroups(t, m, "zone", "zone/nested")
			missingTool := "codex"
			m.services.cfg.CLIs["claude"] = config.CLI{Command: testfixture.Cat(), DefaultStatus: status.Idle}
			m.services.cfg.CLIs[missingTool] = config.CLI{Command: "am-missing-cli-564", DefaultStatus: status.Idle}
			first := store.Session{ID: newID(), Name: "first", Group: "zone", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead, Archived: true}
			second := store.Session{ID: newID(), Name: "second", Group: "zone/nested", Tool: missingTool, Cwd: t.TempDir(), Status: status.Dead, Archived: true}
			if failFirst {
				first.Tool, second.Tool = second.Tool, first.Tool
			}
			sleeper := store.Session{ID: newID(), Name: "sleeper", Group: "zone", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead}
			for _, sess := range []store.Session{first, second, sleeper} {
				if err := m.services.store.CreateSession(sess); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.services.store.SetGroupArchived("zone/nested", true); err != nil {
				t.Fatal(err)
			}
			m.rail.SetArchived(true)
			m.applyCmd(t, m.refreshCmd())
			m.selectGroupRow(t, "zone")
			m.restoreSelected()
			confirmYes(t, m)
			for attempt := 0; attempt < 2; attempt++ {
				fix, open := m.launchHintFix()
				if !open {
					t.Fatalf("attempt %d: no launch hint, error=%s", attempt, m.errBar.text)
				}
				for _, sess := range []store.Session{first, second} {
					stored, err := m.services.store.Get(sess.ID)
					if err != nil || !stored.Archived || m.services.tmux.Exists(sess.ID) {
						t.Fatalf("failed restore left pane/membership: %+v %v", stored, err)
					}
					if _, ok := m.ledger.lifecycle.Launched[sess.ID]; ok {
						t.Fatal("rolled-back pane retained a launch receipt")
					}
				}
				if fix.Retry == nil {
					t.Fatal("missing group retry")
				}
				retry := fix.Retry
				m.Pop()
				if attempt == 1 {
					m.services.cfg.CLIs[missingTool] = config.CLI{Command: testfixture.Cat(), DefaultStatus: status.Idle}
				}
				m.enqueueEffect(retry, 0, false)
				m.drainEffects(t)
			}
			for _, sess := range []store.Session{first, second} {
				stored, err := m.services.store.Get(sess.ID)
				if err != nil || stored.Archived || !m.services.tmux.Exists(sess.ID) {
					t.Fatalf("group retry incomplete: %+v %v", stored, err)
				}
			}
			groups, err := m.services.store.Groups()
			if err != nil {
				t.Fatal(err)
			}
			for _, group := range groups {
				if (group.Name == "zone" || group.Name == "zone/nested") && group.Archived {
					t.Fatalf("group still archived: %+v", group)
				}
			}
			if m.services.tmux.Exists(sleeper.ID) {
				t.Fatal("restore revived intentionally killed active sibling")
			}
		})
	}
}

func TestNestedRestoreAncestorFailureRollsBackAndRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	m := buildModelWithStorePath(t, path)
	seedGroups(t, m, "a", "a/b")
	dead := store.Session{ID: newID(), Name: "dead", Tool: "claude", Cwd: t.TempDir(), Group: "a/b", Status: status.Dead}
	live := store.Session{ID: newID(), Name: "live", Tool: "claude", Cwd: t.TempDir(), Group: "a/b", Status: status.Idle}
	for _, sess := range []store.Session{dead, live} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.services.store.SetGroupArchived("a", true); err != nil {
		t.Fatal(err)
	}
	if err := m.services.tmux.Create(live.ID, live.Cwd, m.services.cfg.CLIs[live.Tool].Command, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	pid, err := m.services.tmux.PanePID(live.ID)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER refuse_ancestor BEFORE UPDATE OF archived ON groups WHEN OLD.name='a' AND NEW.archived=0 BEGIN SELECT RAISE(ABORT,'ancestor refused'); END`); err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "a/b")
	m.restoreSelected()
	confirmYes(t, m)
	if !strings.Contains(m.errBar.text, "ancestor refused") || m.services.tmux.Exists(dead.ID) {
		t.Fatalf("failed restore leaked launch: %s", m.errBar.text)
	}
	for _, sess := range []store.Session{dead, live} {
		stored, err := m.services.store.Get(sess.ID)
		if err != nil || !stored.Archived {
			t.Fatalf("failed ancestor commit unarchived row: %+v %v", stored, err)
		}
	}
	if !m.workspace.archivedGroups["a"] || !m.workspace.archivedGroups["a/b"] {
		t.Fatal("failed commit changed local group flags")
	}
	if _, err := db.Exec(`DROP TRIGGER refuse_ancestor`); err != nil {
		t.Fatal(err)
	}
	m.restoreSelected()
	confirmYes(t, m)
	if after, err := m.services.tmux.PanePID(live.ID); err != nil || after != pid {
		t.Fatalf("retry replaced preexisting pane: %d %v", after, err)
	}
	m.rail.SetArchived(false)
	if !m.focusSession(dead.ID) || !m.focusSession(live.ID) {
		t.Fatalf("retry still hidden: %s", m.errBar.text)
	}
}

func TestNestedRestoreInstallCompletesCapturedGroup(t *testing.T) {
	for _, method := range []string{"key", "mouse"} {
		t.Run(method, func(t *testing.T) {
			command := fakeInstallCommand(t)
			m := buildModel(t)
			m.layout.width, m.layout.height = 160, 40
			seedGroups(t, m, "a", "a/b", "a/b/deep", "a/sibling")
			first := store.Session{ID: newID(), Name: "first", Tool: "claude", Cwd: t.TempDir(), Group: "a/b", Status: status.Dead, Archived: true}
			second := store.Session{ID: newID(), Name: "missing", Tool: installRetryTool, Cwd: t.TempDir(), Group: "a/b/deep", Status: status.Dead, Archived: true}
			sleeper := store.Session{ID: newID(), Name: "sleeper", Tool: "claude", Cwd: t.TempDir(), Group: "a/sibling", Status: status.Dead}
			if err := m.services.store.SetGroupArchived("a", true); err != nil {
				t.Fatal(err)
			}
			for _, sess := range []store.Session{first, second, sleeper} {
				if err := m.services.store.CreateSession(sess); err != nil {
					t.Fatal(err)
				}
			}
			m.services.cfg.CLIs[installRetryTool] = config.CLI{Command: "am-fake-cli", DefaultStatus: status.Idle}
			m.rail.SetArchived(true)
			m.applyCmd(t, m.refreshCmd())
			m.selectGroupRow(t, "a/b")
			m.restoreSelected()
			confirmYes(t, m)
			fix, open := m.launchHintFix()
			if !open || fix.Retry == nil {
				t.Fatalf("missing captured retry: open=%v error=%s", open, m.errBar.text)
			}
			if m.services.tmux.Exists(first.ID) || !m.workspace.archivedGroups["a"] {
				t.Fatal("failed nested restore did not roll back")
			}
			// The fixture replaces only the installer recipe; the refused group request stays intact.
			fix.Command, fix.Script = command, command
			m.presentLaunchHint(*fix)
			if method == "key" {
				m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
			} else {
				m = clickLabel(t, m, "i install")
			}
			shell := terminalSession(t, m)
			if shell.Group != "" || m.rail.ShowArchived() {
				t.Fatalf("repair shell hidden in archive: %+v", shell)
			}
			waitForInstallToSettle(t, m)
			for _, sess := range []store.Session{first, second} {
				stored, err := m.services.store.Get(sess.ID)
				if err != nil || stored.Archived || !m.services.tmux.Exists(sess.ID) || !m.focusSession(sess.ID) {
					t.Fatalf("group retry did not reveal %s: %+v %v error=%s", sess.Name, stored, err, m.errBar.text)
				}
			}
			if m.services.tmux.Exists(sleeper.ID) {
				t.Fatal("install retry revived unrelated dead session")
			}
			if m.groupEffectivelyArchived("a/b") {
				t.Fatal("retry left archived ancestor")
			}
		})
	}
}
