// Package effects is the UI's ordered work queue: FIFO lanes of jobs that
// run off the Bubble Tea update path, the lifetime that admits and drains
// them, and the bookkeeping a quit reads. It knows no request type; the root
// names each request, picks its lane and says which jobs may not overlap.
package effects

import (
	"errors"
	"slices"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ErrStopped is the completion of a job that reached the worker after the
// queue stopped admitting work.
var ErrStopped = errors.New("UI effects have stopped")

// Request is the work a job carries. The root's request types implement it.
type Request interface{ EffectRequest() }

// Result is what a job's work returns.
type Result interface{ EffectResult() }

// Completed is the message a job's command returns.
type Completed struct {
	ID         uint64
	Result     Result
	Err        error
	FinishedAt time.Time
}

// Job is one queued unit of work. Chain groups jobs that one decision
// queued, so a failure can drop the rest of its chain.
type Job struct {
	ID, Chain uint64
	Request   Request
	Command   tea.Cmd
}

// Lane runs one job at a time, in order.
type Lane struct {
	Active  *Job
	Pending []*Job
}

// Lifetime admits work until the queue stops, and counts what is running.
type Lifetime struct {
	mu      sync.Mutex
	closed  bool
	running sync.WaitGroup
}

// Begin admits one job, false once the queue has stopped.
func (l *Lifetime) Begin() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return false
	}
	l.running.Add(1)
	return true
}

// Done ends a job Begin admitted.
func (l *Lifetime) Done() { l.running.Done() }

// CloseAndWait refuses new work and waits up to limit for running work,
// reporting whether it all finished.
func (l *Lifetime) CloseAndWait(limit time.Duration) bool {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	finished := make(chan struct{})
	go func() {
		l.running.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return true
	case <-time.After(limit):
		return false
	}
}

// Command wraps work as the job's tea.Cmd. The work runs at most once; a
// second call returns the first completion, so a command batched twice
// never repeats a write.
func Command(id uint64, lifetime *Lifetime, work func() (Result, error)) tea.Cmd {
	var once sync.Once
	var completed Completed
	return func() tea.Msg {
		once.Do(func() {
			completed.ID = id
			if !lifetime.Begin() {
				completed.Err = ErrStopped
				return
			}
			defer lifetime.Done()
			completed.Result, completed.Err = work()
			completed.FinishedAt = time.Now()
		})
		return completed
	}
}

// Queue runs effects on three FIFO lanes. Input forwarded to a pane has its
// own lane so a keystroke never waits behind a spawn, fork or poll-locked
// reflow, and calls to SSH connections have the third, so a slow host holds
// up only its own kind of work.
type Queue struct {
	NextID, NextChain   uint64
	Main, Input, Remote Lane
	Lifetime            *Lifetime
	// Quitting refuses new work while the accepted work drains.
	Quitting  bool
	Abandoned []string
}

// Lanes is every lane, in the order a quit drain waits on them.
func (q *Queue) Lanes() []*Lane { return []*Lane{&q.Main, &q.Input, &q.Remote} }

// Push queues work on lane, at the front when first is set: a follow-up
// runs before what was queued after the job that asked for it.
func (q *Queue) Push(lane *Lane, request Request, chain uint64, first bool, work func() (Result, error)) {
	if q.Lifetime == nil {
		q.Lifetime = &Lifetime{}
	}
	q.NextID++
	job := &Job{ID: q.NextID, Chain: chain, Request: request, Command: Command(q.NextID, q.Lifetime, work)}
	if first {
		lane.Pending = append([]*Job{job}, lane.Pending...)
	} else {
		lane.Pending = append(lane.Pending, job)
	}
}

// Next starts what each idle lane may run. An input reads its session and
// then writes to it, so an input and a main-lane job that kills or
// relaunches the same pane never overlap: conflict reports that pair, and
// input to any other pane goes ahead. Once a quit has drained every lane,
// Next quits.
func (q *Queue) Next(conflict func(main, input *Job) bool) tea.Cmd {
	var commands []tea.Cmd
	main, input, remote := &q.Main, &q.Input, &q.Remote
	if main.Active == nil && len(main.Pending) > 0 && (input.Active == nil || !conflict(main.Pending[0], input.Active)) {
		main.Active, main.Pending = main.Pending[0], main.Pending[1:]
		commands = append(commands, main.Active.Command)
	}
	if remote.Active == nil && len(remote.Pending) > 0 {
		remote.Active, remote.Pending = remote.Pending[0], remote.Pending[1:]
		commands = append(commands, remote.Active.Command)
	}
	if input.Active == nil {
		for i, job := range input.Pending {
			if main.Active != nil && conflict(main.Active, job) {
				continue
			}
			input.Active = job
			input.Pending = slices.Delete(input.Pending, i, i+1)
			commands = append(commands, job.Command)
			break
		}
	}
	if main.Active == nil && input.Active == nil && remote.Active == nil && q.Quitting {
		return tea.Quit
	}
	return tea.Batch(commands...)
}

// Finish takes the job a completion names off its lane; nil when no lane
// is running it.
func (q *Queue) Finish(id uint64) *Job {
	var job *Job
	for _, lane := range q.Lanes() {
		if lane.Active != nil && lane.Active.ID == id {
			job, lane.Active = lane.Active, nil
		}
	}
	return job
}

// FirstUnfinished is the job a quit drain waits on first: the running
// main-lane job, then its queue, then the other lanes.
func (q *Queue) FirstUnfinished() (*Job, bool) {
	for _, lane := range q.Lanes() {
		if lane.Active != nil {
			return lane.Active, true
		}
		if len(lane.Pending) > 0 {
			return lane.Pending[0], true
		}
	}
	return nil, false
}

// Abandon answers a second quit while the drain waits: it drops the queues
// and records what may or may not have happened, rather than leaving a stuck
// effect as the only way out being kill -9.
func (q *Queue) Abandon(name func(Request) string) tea.Cmd {
	for _, lane := range q.Lanes() {
		if lane.Active != nil {
			q.Abandoned = append(q.Abandoned, name(lane.Active.Request)+" was still running")
		}
		for _, job := range lane.Pending {
			q.Abandoned = append(q.Abandoned, name(job.Request)+" never started")
		}
		lane.Pending = nil
	}
	return tea.Quit
}

// Stop refuses new work and waits up to limit for running work; a quit the
// user forced waits for nothing. Work that outlives the wait is recorded in
// Abandoned.
func (q *Queue) Stop(limit time.Duration, name func(Request) string) {
	if q.Lifetime == nil {
		return
	}
	if len(q.Abandoned) > 0 {
		limit = 0
	}
	if !q.Lifetime.CloseAndWait(limit) && len(q.Abandoned) == 0 {
		for _, lane := range q.Lanes() {
			if lane.Active != nil {
				q.Abandoned = append(q.Abandoned, name(lane.Active.Request)+" was still running")
			}
		}
	}
}
