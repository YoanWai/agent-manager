package ui

import (
	"runtime"
	"testing"
	"time"
)

func TestEffectLifetimeWaitsForRunningWork(t *testing.T) {
	lifetime := &effectLifetime{}
	if !lifetime.begin() {
		t.Fatal("fresh lifetime refused work")
	}
	started := make(chan struct{})
	stopped := make(chan struct{})
	go func() { close(started); lifetime.closeAndWait(); close(stopped) }()
	<-started
	deadline := time.Now().Add(time.Second)
	for lifetime.begin() {
		lifetime.running.Done()
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
	lifetime.running.Done()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("did not drain")
	}
	if lifetime.begin() {
		t.Fatal("closed lifetime admitted work")
	}
}
