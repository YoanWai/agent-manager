package ui

import (
	uionboarding "github.com/YoanWai/agent-manager/internal/ui/onboarding"
	onboardingruntime "github.com/YoanWai/agent-manager/internal/ui/onboarding/runtime"
	"testing"
)

func TestSetupIgnoresCompletionsFromThePreviousVisit(t *testing.T) {
	m := buildModel(t)
	m.openSetup(true)
	previous, _ := m.setupSurface()
	m.Pop()
	m.openSetup(true)
	current, _ := m.setupSurface()
	if cmd := m.applySetupProbe(onboardingruntime.ProbeRequest{Target: previous.ID()}, onboardingruntime.ProbeResult{Installed: map[string]bool{"claude": true}}, nil); cmd != nil || current.Probed() {
		t.Fatal("the old visit's probe landed on the new setup")
	}
	if _, cmd, _ := m.routeSetupMsg(uionboarding.TickMsg{Surface: previous}); cmd != nil || current.Ticking() {
		t.Fatal("the old visit's tick started the new setup")
	}
	pending := m.settings.Pending
	if _, cmd, _ := m.routeSetupMsg(uionboarding.OutcomeMsg{Surface: previous}); cmd != nil || m.settings.Pending != pending {
		t.Fatal("the old visit's input changed the new setup")
	}
}

func TestLateSetupOpenerDoesNotCoverANewerCard(t *testing.T) {
	m := buildModel(t)
	msg := openSetupMsg{Screen: m.screenID(), ForegroundGen: m.gens.foreground}
	m.openHelp()
	m.routeSetupMsg(msg)
	if m.setupOpen() {
		t.Fatal("the delayed Settings action covered the newer Help card")
	}
}
