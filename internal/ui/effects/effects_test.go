package effects

import (
	"runtime"
	"testing"
	"time"
)

func TestEffectLifetimeWaitsForRunningWork(t *testing.T) {
	lifetime := &Lifetime{}
	if !lifetime.Begin() {
		t.Fatal("fresh lifetime refused work")
	}
	started := make(chan struct{})
	stopped := make(chan struct{})
	go func() { close(started); lifetime.CloseAndWait(time.Minute); close(stopped) }()
	<-started
	deadline := time.Now().Add(time.Second)
	for lifetime.Begin() {
		lifetime.Done()
		if time.Now().After(deadline) {
			t.Fatal("shutdown did not close admission")
		}
		runtime.Gosched()
	}
	select {
	case <-stopped:
		t.Fatal("closed while work was running")
	default:
	}
	lifetime.Done()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("did not drain")
	}
	if lifetime.Begin() {
		t.Fatal("closed lifetime admitted work")
	}
}

func TestEffectLifetimeWaitIsBounded(t *testing.T) {
	lifetime := &Lifetime{}
	lifetime.Begin()
	if lifetime.CloseAndWait(10 * time.Millisecond) {
		t.Fatal("reported running work as finished")
	}
}
