package launch

import (
	"slices"
	"time"

	"github.com/YoanWai/agent-manager/internal/git"
	tea "github.com/charmbracelet/bubbletea"
)

type BaseTarget uint8

const (
	BaseForm BaseTarget = iota
	BaseRename
)

// BaseRefsTTL bounds how long a repo's branch list steps a base picker
// without a fresh read, so a held arrow steps at key speed.
const BaseRefsTTL = 10 * time.Second

type BaseRefsAnswer struct {
	Refs []string
	At   time.Time
}

// BaseStepMsg is a base picker's step, answered off the update path with the
// branches of Dir. It lands only on the picker that asked, while it still
// shows From.
type BaseStepMsg struct {
	Target BaseTarget
	Gen    uint64
	Dir    string
	From   string
	Delta  int
	Refs   []string
	Err    error
}

// StepGroupBase moves a group's base choice through auto and the branches of
// the repo at dir, reporting the next value and the command that reads the
// branches when the memo is too old to step at once.
func (s *State) StepGroupBase(driver *git.Driver, target BaseTarget, gen uint64, dir, current string, delta int, now time.Time) (string, tea.Cmd, error) {
	if answer, ok := s.BaseRefs[dir]; ok && now.Sub(answer.At) < BaseRefsTTL {
		return StepBaseChoice(answer.Refs, current, delta), nil, nil
	}
	if driver == nil {
		return current, nil, ErrGroupBaseNeedsGit
	}
	return current, func() tea.Msg {
		refs, err := driver.BranchRefs(dir)
		return BaseStepMsg{Target: target, Gen: gen, Dir: dir, From: current, Delta: delta, Refs: refs, Err: err}
	}, nil
}

var ErrGroupBaseNeedsGit = errGroupBaseNeedsGit{}

type errGroupBaseNeedsGit struct{}

func (errGroupBaseNeedsGit) Error() string { return "a group base needs git installed" }

func StepBaseChoice(refs []string, current string, delta int) string {
	choices := append([]string{""}, refs...)
	at := max(slices.Index(choices, current), 0)
	return choices[(at+delta+len(choices))%len(choices)]
}

// KeepBaseRefs keeps a repo's branch list, so the next step is a memo read.
func (s *State) KeepBaseRefs(dir string, refs []string, now time.Time) {
	if s.BaseRefs == nil {
		s.BaseRefs = map[string]BaseRefsAnswer{}
	}
	s.BaseRefs[dir] = BaseRefsAnswer{Refs: refs, At: now}
}

// BaseFetchInterval keeps a burst of spawns into one repo to one fetch.
const BaseFetchInterval = time.Minute

// BaseFetchKey names one repo's base refresh: its directory and the base
// override a group set.
type BaseFetchKey struct{ Dir, Override string }

// BaseFetch is one refresh of a spawn's base: when it started, the default
// branch the repo resolved to, and how the fetch ended.
// LookupErr is the default base failing to resolve, Err the fetch failing.
type BaseFetch struct {
	At        time.Time
	Resolved  bool
	Detected  string
	LookupErr error
	Fetched   bool
	Err       error
}

type BaseFetchedMsg struct {
	Key       BaseFetchKey
	Detected  string
	LookupErr error
	Fetched   bool
	Err       error
}

// ReserveBaseFetch claims one repo's refresh, reporting false when a recent
// one already ran.
func (s *State) ReserveBaseFetch(key BaseFetchKey, now time.Time) bool {
	if last, seen := s.BaseFetches[key]; seen && now.Sub(last.At) < BaseFetchInterval {
		return false
	}
	if s.BaseFetches == nil {
		s.BaseFetches = map[BaseFetchKey]BaseFetch{}
	}
	s.BaseFetches[key] = BaseFetch{At: now}
	return true
}

// ResolveBaseCmd reads the repo's default branch off the update path.
func ResolveBaseCmd(driver *git.Driver, key BaseFetchKey) tea.Cmd {
	return func() tea.Msg {
		detected, err := driver.DefaultBase(key.Dir)
		return BaseFetchedMsg{Key: key, Detected: detected, LookupErr: err}
	}
}

// RecordBaseFetch keeps what a step of a base refresh found, and returns the
// fetch command once the resolving step is in.
func (s *State) RecordBaseFetch(msg BaseFetchedMsg, driver *git.Driver, fetchOff bool) tea.Cmd {
	fetch, ok := s.BaseFetches[msg.Key]
	if !ok {
		return nil
	}
	fetch.Resolved, fetch.Detected, fetch.LookupErr = true, msg.Detected, msg.LookupErr
	if msg.Fetched {
		fetch.Fetched, fetch.Err = true, msg.Err
	}
	s.BaseFetches[msg.Key] = fetch
	if msg.Fetched || fetchOff {
		return nil
	}
	key := msg.Key
	return func() tea.Msg {
		err := driver.FetchBase(key.Dir, key.Override)
		detected, lookupErr := driver.DefaultBase(key.Dir)
		return BaseFetchedMsg{Key: key, Detected: detected, LookupErr: lookupErr, Fetched: true, Err: err}
	}
}

func (s *State) BaseFetch(dir, override string) BaseFetch {
	return s.BaseFetches[BaseFetchKey{Dir: dir, Override: override}]
}
