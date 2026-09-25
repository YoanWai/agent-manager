package store

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestSetGroupArchivedFlipsSubtree(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("proj", "")
	st.CreateGroup("proj/sub", "")
	st.CreateSession(sample("a", "proj"))
	st.CreateSession(sample("b", "proj/sub"))

	if err := st.SetGroupArchived("proj", true); err != nil {
		t.Fatalf("archive group: %v", err)
	}
	if !groupArchived(t, st, "proj") || !groupArchived(t, st, "proj/sub") {
		t.Fatal("group and subgroup should be archived")
	}
	if !sessionArchived(t, st, "a") || !sessionArchived(t, st, "b") {
		t.Fatal("sessions in subtree should be archived")
	}

	if err := st.SetGroupArchived("proj", false); err != nil {
		t.Fatalf("restore group: %v", err)
	}
	if groupArchived(t, st, "proj") || groupArchived(t, st, "proj/sub") {
		t.Fatal("group and subgroup should be restored")
	}
	if sessionArchived(t, st, "a") || sessionArchived(t, st, "b") {
		t.Fatal("sessions in subtree should be restored")
	}
}

func TestSetGroupArchivedUnderscoreDoesNotBleed(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("my_proj", "")
	st.CreateGroup("myXproj/sub", "")
	st.CreateSession(sample("a", "my_proj"))
	st.CreateSession(sample("b", "myXproj/sub"))

	if err := st.SetGroupArchived("my_proj", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !groupArchived(t, st, "my_proj") || !sessionArchived(t, st, "a") {
		t.Fatal("my_proj and its session should be archived")
	}
	// "my_proj/%" with an unescaped underscore matches "myXproj/sub".
	if groupArchived(t, st, "myXproj/sub") || sessionArchived(t, st, "b") {
		t.Fatal("myXproj/sub must not be caught by the my_proj archive (LIKE _ wildcard bleed)")
	}
}

func TestPruneArchivedGroupsRemovesOnlyEmptyArchivedOnes(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("proj", "")
	st.CreateGroup("proj/gone", "")
	st.CreateGroup("proj/live", "")
	st.CreateSession(sample("a", "proj/live"))
	if err := st.SetGroupArchived("proj/gone", true); err != nil {
		t.Fatalf("archive group: %v", err)
	}

	removed, err := st.PruneArchivedGroups("proj")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(removed) != 1 || removed[0] != "proj/gone" {
		t.Fatalf("removed = %v want [proj/gone]", removed)
	}
	paths := groupPaths(t, st)
	if len(paths) != 2 || paths[0] != "proj" || paths[1] != "proj/live" {
		t.Fatalf("remaining groups = %v want [proj proj/live]", paths)
	}
}

func TestPruneArchivedGroupsKeepsArchivedGroupHoldingASession(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("proj", "")
	st.CreateGroup("proj/sub", "")
	if err := st.SetGroupArchived("proj", true); err != nil {
		t.Fatalf("archive group: %v", err)
	}
	// A session launched into an archived group starts out live, so neither
	// its group nor that group's ancestors may be pruned away under it.
	st.CreateSession(sample("a", "proj/sub"))

	removed, err := st.PruneArchivedGroups("proj")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v want none", removed)
	}
	if paths := groupPaths(t, st); len(paths) != 2 {
		t.Fatalf("remaining groups = %v want both kept", paths)
	}
}

func TestSetGroupArchivedEmptyPathErrors(t *testing.T) {
	st := newTestStore(t)
	if err := st.SetGroupArchived("", true); err == nil {
		t.Fatal("archiving the root group should error")
	}
}

func TestGroupWorktreeRoundtrip(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("backend", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Worktree != "" {
		t.Fatalf("new group should inherit worktree, got %+v", groups)
	}
	if err := st.SetGroupWorktree("backend", "on"); err != nil {
		t.Fatalf("set: %v", err)
	}
	groups, err = st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if groups[0].Worktree != "on" {
		t.Fatalf("worktree choice lost: %+v", groups[0])
	}
	if err := st.SetGroupWorktree("backend", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	groups, err = st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if groups[0].Worktree != "" {
		t.Fatalf("worktree choice should clear back to inherit: %+v", groups[0])
	}
}

func TestAddGroupStoresSettingsWithoutReplacingExistingGroup(t *testing.T) {
	st := newTestStore(t)
	if err := st.AddGroup("backend", "/first", "off"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := st.AddGroup("backend", "/second", "on"); !errors.Is(err, ErrGroupExists) {
		t.Fatalf("duplicate add error = %v, want ErrGroupExists", err)
	}

	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Path != "/first" || groups[0].Worktree != "off" {
		t.Fatalf("duplicate add changed group: %+v", groups)
	}
}

func TestMoveGroupReparentsSubtree(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("alpha/inner", "/srv/inner"); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.CreateGroup("alpha/inner/deep", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.CreateGroup("beta", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.SetGroupWorktree("alpha/inner", "on"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	if err := st.CreateSession(sample("a", "alpha/inner")); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.CreateSession(sample("b", "alpha/inner/deep")); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.MoveGroup("alpha/inner", "beta"); err != nil {
		t.Fatalf("MoveGroup: %v", err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	byName := make(map[string]Group, len(groups))
	for _, group := range groups {
		if group.Name == "alpha/inner" || group.Name == "alpha/inner/deep" {
			t.Fatalf("old group path %s still present", group.Name)
		}
		byName[group.Name] = group
	}
	moved, ok := byName["beta/inner"]
	if !ok {
		t.Fatal("beta/inner missing")
	}
	if moved.Path != "/srv/inner" {
		t.Fatalf("beta/inner path = %q, want /srv/inner", moved.Path)
	}
	if moved.Worktree != "on" {
		t.Fatalf("beta/inner worktree = %q, want on", moved.Worktree)
	}
	if _, ok := byName["beta/inner/deep"]; !ok {
		t.Fatal("beta/inner/deep missing")
	}
	for id, want := range map[string]string{"a": "beta/inner", "b": "beta/inner/deep"} {
		sess, err := st.Get(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if sess.Group != want {
			t.Fatalf("session %s group = %q, want %q", id, sess.Group, want)
		}
	}
}

func TestMoveGroupToRoot(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.MoveGroup("alpha/inner", ""); err != nil {
		t.Fatalf("MoveGroup: %v", err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	found := false
	for _, group := range groups {
		if group.Name == "inner" {
			found = true
		}
	}
	if !found {
		t.Fatal("group inner missing at root")
	}
}

func TestMoveGroupIntoOwnSubtreeFails(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.MoveGroup("alpha", "alpha/inner"); err == nil {
		t.Fatal("moving a group into its own subtree should fail")
	}
	if err := st.MoveGroup("alpha", "alpha"); err == nil {
		t.Fatal("moving a group into itself should fail")
	}
}

func TestMoveGroupNameCollisionFails(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.CreateGroup("beta/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.MoveGroup("alpha/inner", "beta"); err == nil {
		t.Fatal("moving onto an existing sibling name should fail")
	}
}

func TestMoveGroupSameParentIsNoop(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.MoveGroup("alpha/inner", "alpha"); err != nil {
		t.Fatalf("MoveGroup: %v", err)
	}
}

func TestRemoveGroupMovesTheSubtreeInOneStroke(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("fleet", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := st.CreateGroup("fleet/backend", ""); err != nil {
		t.Fatalf("CreateGroup nested: %v", err)
	}
	if err := st.CreateSession(sample("aa", "fleet")); err != nil {
		t.Fatalf("create: %v", err)
	}
	nested := sample("bb", "fleet/backend")
	nested.ParentID = "aa"
	if err := st.CreateSession(nested); err != nil {
		t.Fatalf("create nested: %v", err)
	}

	removed, moved, err := st.RemoveGroup("fleet")
	if err != nil {
		t.Fatalf("RemoveGroup: %v", err)
	}
	sort.Strings(removed)
	if !slices.Equal(removed, []string{"fleet", "fleet/backend"}) {
		t.Fatalf("removed = %v", removed)
	}
	sort.Strings(moved)
	if !slices.Equal(moved, []string{"aa", "bb"}) {
		t.Fatalf("moved = %v", moved)
	}
	after, err := st.Get("bb")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Group != "" || after.ParentID != "aa" {
		t.Fatalf("nested session moved to %q under %q; want the root under aa", after.Group, after.ParentID)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("Groups: %v", err)
	}
	for _, g := range groups {
		if strings.HasPrefix(g.Name, "fleet") {
			t.Fatalf("group %q survived", g.Name)
		}
	}
}

func TestRemoveGroupReportsAMissingGroup(t *testing.T) {
	st := newTestStore(t)
	if _, _, err := st.RemoveGroup("ghost"); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("err = %v, want ErrGroupNotFound", err)
	}
	if _, _, err := st.RemoveGroup(""); err == nil {
		t.Fatal("removing the root group was accepted")
	}
}

// A group name may carry LIKE wildcards; they must not pull a sibling
// group's sessions into the move.
func TestRemoveGroupMatchesWildcardNamesLiterally(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("a_c", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := st.CreateGroup("abc", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := st.CreateSession(sample("cc", "abc/sub")); err != nil {
		t.Fatalf("create: %v", err)
	}
	removed, moved, err := st.RemoveGroup("a_c")
	if err != nil {
		t.Fatalf("RemoveGroup: %v", err)
	}
	if len(moved) != 0 {
		t.Fatalf("moved = %v, want nothing from the sibling group", moved)
	}
	if !slices.Equal(removed, []string{"a_c"}) {
		t.Fatalf("removed = %v", removed)
	}
}
