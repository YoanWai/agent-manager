package execution

import (
	"context"
	"errors"
	"github.com/YoanWai/agent-manager/internal/store"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunCancellationClosesResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := New(Dependencies{}, Options{Interval: time.Hour})
	select {
	case _, open := <-runner.Run(ctx):
		if open {
			t.Fatal("canceled runner produced a result")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled runner did not close its result channel")
	}
}

func TestBlockedSubscriberDoesNotBlockCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &Runner{interval: time.Millisecond, poke: make(chan struct{}, 1)}
	var count atomic.Int32
	results := runner.run(ctx, func() Result { n := count.Add(1); return Result{Snapshot: Snapshot{Agents: AgentStats{Count: int(n)}}} })
	deadline := time.Now().Add(time.Second)
	for count.Load() < 4 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("blocked subscriber stopped runtime steps")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-time.After(time.Second):
		t.Fatal("runner blocked on an unread result")
	case result := <-results:
		if result.Snapshot.Agents.Count < 3 {
			t.Fatalf("blocked subscriber stopped steps: count=%d", result.Snapshot.Agents.Count)
		}
	}
	select {
	case _, open := <-results:
		if open {
			t.Fatal("runner produced another result after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not close after cancellation")
	}
}

func TestCancellationWaitsForOutstandingCapture(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := New(Dependencies{}, Options{Interval: time.Hour})
	runner.captureWait.Add(1)
	results := runner.Run(ctx)
	select {
	case <-results:
		t.Fatal("runner closed before capture completed")
	case <-time.After(10 * time.Millisecond):
	}
	runner.captureWait.Done()
	select {
	case _, open := <-results:
		if open {
			t.Fatal("canceled runner produced a result")
		}
	case <-time.After(time.Second):
		t.Fatal("capture completion did not release shutdown")
	}
}

func TestRunnerRejectsASecondRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := New(Dependencies{}, Options{Interval: time.Hour})
	first := runner.run(ctx, func() Result { return Result{} })
	second := runner.run(ctx, func() Result { t.Fatal("second execution loop started"); return Result{} })
	if result := <-second; result.Err == nil {
		t.Fatal("second run was accepted")
	}
	cancel()
	for range first {
	}
}

func TestCanceledRunnerRefusesFurtherRuntimeActions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := New(Dependencies{}, Options{})
	for range runner.Run(ctx) {
	}
	if result := runner.Step(); result.Err == nil {
		t.Fatal("stopped runtime allowed another step")
	}
	ran := false
	runner.ReflowSessions([]string{"fixture"}, func() { ran = true })
	if ran {
		t.Fatal("stopped runtime dispatched a reflow")
	}
	if err := runner.TypeForkKeys(store.Session{}, ""); err == nil {
		t.Fatal("stopped runtime dispatched fork keys")
	}
}

func TestBlockedSubscriberRetainsAnErrorUntilObserved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := New(Dependencies{}, Options{Interval: time.Hour})
	started := make(chan struct{})
	steps := make(chan Result)
	results := runner.run(ctx, func() Result {
		select {
		case started <- struct{}{}:
		case <-ctx.Done():
			return Result{}
		}
		select {
		case result := <-steps:
			return result
		case <-ctx.Done():
			return Result{}
		}
	})
	defer func() {
		cancel()
		for range results {
		}
	}()
	failed := errors.New("delivery failed")
	for _, result := range []Result{{Err: failed}, {Snapshot: Snapshot{Agents: AgentStats{Count: 2}}}, {Snapshot: Snapshot{Agents: AgentStats{Count: 3}}}} {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("runtime stopped stepping")
		}
		steps <- result
		runner.RequestRefresh()
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("runtime stopped stepping")
	}
	if result := <-results; !errors.Is(result.Err, failed) {
		t.Fatalf("unread delivery error was replaced: %+v", result)
	}
	steps <- Result{Snapshot: Snapshot{Agents: AgentStats{Count: 4}}}
	select {
	case result := <-results:
		if result.Err != nil || result.Snapshot.Agents.Count != 4 {
			t.Fatalf("next observation = %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not resume snapshots")
	}
}
