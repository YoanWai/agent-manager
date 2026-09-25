package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

var ErrGroupExists = errors.New("group already exists")

// ensureGroup registers a group by name if it does not exist, leaving any
// existing default path untouched. The empty root is never stored.
func (s *Store) ensureGroup(name string) error {
	if name == "" {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO groups (name, sort_order)
		 VALUES (?, (SELECT COALESCE(MAX(sort_order)+1, 0) FROM groups))
		 ON CONFLICT(name) DO NOTHING`, name)
	return err
}

// CreateGroup registers a group path like "backend/api/auth" with an
// optional default working directory, updating the path if it already exists.
func (s *Store) CreateGroup(name, path string) error {
	if name == "" {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO groups (name, path, sort_order)
		 VALUES (?, ?, (SELECT COALESCE(MAX(sort_order)+1, 0) FROM groups))
		 ON CONFLICT(name) DO UPDATE SET path = excluded.path`, name, path)
	return err
}

func (s *Store) AddGroup(name, path, worktree string) error {
	if name == "" {
		return errors.New("group name cannot be empty")
	}
	res, err := s.db.Exec(
		`INSERT INTO groups (name, path, worktree, sort_order)
		 VALUES (?, ?, ?, (SELECT COALESCE(MAX(sort_order)+1, 0) FROM groups))
		 ON CONFLICT(name) DO NOTHING`, name, path, worktree)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("group %q: %w", name, ErrGroupExists)
	}
	return nil
}

// unarchiveAncestorGroups clears the archived flag on a group path and each
// of its ancestors, leaving descendants untouched.
func (s *Store) unarchiveAncestorGroups(path string) error {
	var err error
	eachAncestor(path, func(ancestor string) bool {
		_, err = s.db.Exec(`UPDATE groups SET archived = 0 WHERE name = ?`, ancestor)
		return err == nil
	})
	return err
}

// RenameGroup rewrites a group path and every descendant group and
// session under it. Fails if the destination path already exists.
func (s *Store) RenameGroup(oldPath, newPath string) error {
	if oldPath == "" || newPath == "" {
		return fmt.Errorf("group path cannot be empty")
	}
	if oldPath == newPath {
		return nil
	}
	var exists int
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM groups WHERE name = ?)`, newPath).Scan(&exists)
	if err != nil {
		return err
	}
	if exists == 1 {
		return fmt.Errorf("group %s already exists", newPath)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	likeOld := escapeLike(oldPath)
	_, err = tx.Exec(
		`UPDATE groups SET name = ? || substr(name, length(?)+1)
		 WHERE name = ? OR name LIKE ? || '/%' ESCAPE '\'`,
		newPath, oldPath, oldPath, likeOld)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`UPDATE sessions SET group_name = ? || substr(group_name, length(?)+1)
		 WHERE group_name = ? OR group_name LIKE ? || '/%' ESCAPE '\'`,
		newPath, oldPath, oldPath, likeOld)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// MoveGroup re-parents a group subtree under newParent ("" = root),
// keeping its base name. Every descendant group and session follows.
func (s *Store) MoveGroup(path, newParent string) error {
	if path == "" {
		return fmt.Errorf("group path cannot be empty")
	}
	if inSubtree(newParent, path) {
		return fmt.Errorf("cannot move %s into its own subtree", path)
	}
	newPath := path[strings.LastIndex(path, "/")+1:]
	if newParent != "" {
		newPath = newParent + "/" + newPath
	}
	return s.RenameGroup(path, newPath)
}

// DeleteGroup removes a group and all its descendant groups, reporting
// the paths it removed.
func (s *Store) DeleteGroup(path string) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("cannot delete the root group")
	}
	groups, err := s.Groups()
	if err != nil {
		return nil, err
	}
	return s.deleteGroups(groups, func(g Group) bool { return inSubtree(g.Name, path) })
}

// ErrGroupNotFound reports a group path no row carries.
var ErrGroupNotFound = errors.New("group does not exist")

// RemoveGroup deletes a group and its descendant groups and moves every
// session held beneath them to the root, in one transaction, so a failure
// partway leaves the tree as it was. A moved session keeps its parent
// link, so a terminal relocated with its agent still hangs under it.
// Reports the group paths removed and the ids of the sessions moved.
func (s *Store) RemoveGroup(path string) (removedGroups, movedSessions []string, err error) {
	if path == "" {
		return nil, nil, fmt.Errorf("cannot delete the root group")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	groupNames, err := txStrings(tx, `SELECT name FROM groups ORDER BY sort_order, name`)
	if err != nil {
		return nil, nil, err
	}
	known := false
	for _, name := range groupNames {
		if name == path {
			known = true
		}
		if inSubtree(name, path) {
			removedGroups = append(removedGroups, name)
		}
	}
	if !known {
		return nil, nil, ErrGroupNotFound
	}
	held, err := txStrings(tx,
		`SELECT id FROM sessions WHERE group_name = ? OR group_name LIKE ? || '/%' ESCAPE '\'
		 ORDER BY group_name, parent_id, sort_order`,
		path, escapeLike(path))
	if err != nil {
		return nil, nil, err
	}
	var nextOrder int
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(sort_order)+1, 0) FROM sessions WHERE group_name = ''`,
	).Scan(&nextOrder); err != nil {
		return nil, nil, err
	}
	for _, id := range held {
		if _, err := tx.Exec(
			`UPDATE sessions SET group_name = '', sort_order = ? WHERE id = ?`, nextOrder, id); err != nil {
			return nil, nil, err
		}
		nextOrder++
	}
	for _, name := range removedGroups {
		if _, err := tx.Exec(`DELETE FROM groups WHERE name = ?`, name); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return removedGroups, held, nil
}

func txStrings(tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// PruneArchivedGroups removes the groups in a subtree that are archived,
// directly or through an archived ancestor, and hold no session anywhere
// beneath them, reporting the paths it removed. A group that still holds
// a session stays, and so does each of its ancestors, so no session is
// left without a home. Deleting a group from the archived view runs this
// instead of DeleteGroup: it clears the group rows that emptying the
// archive left behind while the live tree keeps its own.
func (s *Store) PruneArchivedGroups(root string) ([]string, error) {
	sessions, err := s.ListSessions(true)
	if err != nil {
		return nil, err
	}
	occupied := map[string]bool{}
	for _, sess := range sessions {
		eachAncestor(sess.Group, func(path string) bool {
			occupied[path] = true
			return true
		})
	}
	groups, err := s.Groups()
	if err != nil {
		return nil, err
	}
	archived := map[string]bool{}
	for _, g := range groups {
		if g.Archived {
			archived[g.Name] = true
		}
	}
	return s.deleteGroups(groups, func(g Group) bool {
		return inSubtree(g.Name, root) && !occupied[g.Name] && EffectivelyArchived(archived, g.Name)
	})
}

// deleteGroups removes every group the predicate selects, reporting the
// paths it removed.
func (s *Store) deleteGroups(groups []Group, selected func(Group) bool) ([]string, error) {
	var removed []string
	for _, g := range groups {
		if !selected(g) {
			continue
		}
		if _, err := s.db.Exec(`DELETE FROM groups WHERE name = ?`, g.Name); err != nil {
			return removed, err
		}
		removed = append(removed, g.Name)
	}
	return removed, nil
}

// EffectivelyArchived reports whether a group path counts as archived,
// either directly or because an ancestor group was archived as a whole.
func EffectivelyArchived(archived map[string]bool, path string) bool {
	found := false
	eachAncestor(path, func(ancestor string) bool {
		found = archived[ancestor]
		return !found
	})
	return found
}

// inSubtree reports whether a group path is the root itself or any group
// nested under it.
func inSubtree(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

// eachAncestor visits a group path and then each of its ancestors,
// stopping early when the visitor returns false.
func eachAncestor(path string, visit func(string) bool) {
	for path != "" {
		if !visit(path) {
			return
		}
		idx := strings.LastIndex(path, "/")
		if idx < 0 {
			return
		}
		path = path[:idx]
	}
}

type Group struct {
	Name     string
	Path     string
	Archived bool
	// Worktree is the group's spawn-in-worktree choice: "on", "off", or
	// "" to inherit from the nearest ancestor with a choice, else the
	// global setting.
	Worktree string
}

func (s *Store) Groups() ([]Group, error) {
	rows, err := s.db.Query(`SELECT name, path, archived, worktree FROM groups ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []Group
	for rows.Next() {
		var g Group
		var archived int
		if err := rows.Scan(&g.Name, &g.Path, &archived, &g.Worktree); err != nil {
			return nil, err
		}
		g.Archived = archived != 0
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// SetGroupWorktree stores a group's spawn-in-worktree choice: "on",
// "off", or "" to inherit.
func (s *Store) SetGroupWorktree(name, worktree string) error {
	_, err := s.db.Exec(`UPDATE groups SET worktree = ? WHERE name = ?`, worktree, name)
	return err
}

// SetGroupArchived flips the archived flag on a group, every descendant
// group, and every session in the subtree, in one transaction.
func (s *Store) SetGroupArchived(path string, archived bool) error {
	if path == "" {
		return fmt.Errorf("cannot archive the root group")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	flag := boolToInt(archived)
	likePath := escapeLike(path)
	if _, err := tx.Exec(
		`UPDATE groups SET archived = ? WHERE name = ? OR name LIKE ? || '/%' ESCAPE '\'`,
		flag, path, likePath); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE sessions SET archived = ? WHERE group_name = ? OR group_name LIKE ? || '/%' ESCAPE '\'`,
		flag, path, likePath); err != nil {
		return err
	}
	return tx.Commit()
}
