package ui

import (
	"time"
)

type launchLedger struct {
	// worktreeRepos memoizes which spawn directories sit inside a git
	// repo, so gating the worktree toggle does not shell out to git on
	// every frame. Entries expire, so a directory git-initialised while
	// the bar is open stops reading as unavailable.
	worktreeRepos map[string]repoAnswer
	// baseRefs memoizes each repo's branches for the group base pickers.
	baseRefs          map[string]baseRefsAnswer
	lastSpawnTool     string
	lastSpawnWorktree bool
	// composerSeq numbers the prompt boxes this run has opened.
	composerSeq int
	// Repo a human picked by hand per session, outranking the agent's
	// declaration for as long as this manager runs.
	pickedRepos map[string]string
	// awaitedRenames holds what a spawned session launched with, for as long
	// as the agent it carries the rename directive to is still expected to
	// answer. A rename that has not landed by the time this manager run ends
	// is one that is never arriving, so the set is deliberately not persisted.
	awaitedRenames map[string]awaitedRename
	// launched is when this run recorded each session it spawned. A poll
	// that listed the store before that has nothing to say about the row.
	launched map[string]time.Time
	// gone is when this run took each session off the loaded list itself,
	// by deleting or archiving it, so a poll that listed the store before
	// that moment cannot put the row back on screen for a frame.
	gone map[string]goneMark
	// goneGroups is the group-path counterpart of gone: when this run
	// archived, restored, or deleted a group, a poll that listed the store
	// before that moment must not put the old state back on the tree.
	goneGroups map[string]goneMark
	// terminalKeyAt is when the last T finished being handled. Held down it
	// autorepeats into a burst of keystrokes, and T is the only key that
	// spawns on the keystroke itself rather than opening a form that would
	// swallow them.
	terminalKeyAt time.Time
	catalogs      map[string]*catalogState
	// baseFetches holds the last fetch of a worktree spawn's base, per
	// directory and base override.
	baseFetches map[baseFetchKey]baseFetch
}

type awaitedRename struct {
	generated string
	prompt    string
}

// repoAnswer is one directory's git-repo verdict and when it was taken.
type repoAnswer struct {
	capable bool
	at      time.Time
}
