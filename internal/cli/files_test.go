package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

func TestReserveParsesPathsAndFlags(t *testing.T) {
	fake := &fakeSessions{reserve: sessioncmd.ReserveResult{
		Reserved: []sessioncmd.Reservation{{Pattern: "internal/cli", Mode: "exclusive"}},
	}}
	out := &bytes.Buffer{}
	args := []string{"internal/cli", "internal/ui", "--mode", "shared", "--note", "wiring", "--ttl", "45m"}
	if err := runReserve(out, fake, args, "cafe0001"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if len(fake.patterns) != 2 || fake.patterns[1] != "internal/ui" {
		t.Fatalf("patterns = %v", fake.patterns)
	}
	if fake.mode != "shared" || fake.note != "wiring" || fake.ttl != 45*time.Minute {
		t.Fatalf("reserve flags = %q %q %s", fake.mode, fake.note, fake.ttl)
	}
	if !strings.Contains(out.String(), "reserved internal/cli") {
		t.Fatalf("reserve output = %q", out.String())
	}

	fake.patterns = nil
	if err := runReserve(&bytes.Buffer{}, fake, []string{"--", "-notaflag"}, "cafe0001"); err == nil {
		t.Fatal("a path that reads as a flag should be refused")
	}
	if fake.patterns != nil {
		t.Fatalf("a refused path reached the layer: %v", fake.patterns)
	}

	// The flags parse wherever they sit, so a leading dash is the whole
	// reason a mistyped one cannot be leased, and the refusal has to say so
	// rather than send the agent to reorder what it typed.
	mistyped := runReserve(&bytes.Buffer{}, fake, []string{"internal/cli", "--mod", "shared"}, "cafe0001")
	if mistyped == nil {
		t.Fatal("a mistyped flag should not be leased as a pattern")
	}
	if !strings.Contains(mistyped.Error(), `"--mod" starts with a dash`) ||
		!strings.Contains(mistyped.Error(), "usage: agent-manager reserve") {
		t.Fatalf("the refusal does not name the cause and the usage: %q", mistyped)
	}
	if fake.patterns != nil {
		t.Fatalf("a mistyped flag reached the layer: %v", fake.patterns)
	}
	if err := runReserve(&bytes.Buffer{}, fake, nil, "cafe0001"); err == nil {
		t.Fatal("reserve needs at least one path")
	}
	if fake.patterns != nil {
		t.Fatalf("an empty reserve reached the layer: %v", fake.patterns)
	}
}

func TestReservationsAndReleaseReadTheSameLeases(t *testing.T) {
	out := &bytes.Buffer{}
	if err := runReservations(out, &fakeSessions{}, nil, "cafe0001"); err != nil {
		t.Fatalf("reservations: %v", err)
	}
	if !strings.Contains(out.String(), "- internal/cli (exclusive) held by api for 30m0s") {
		t.Fatalf("reservations output = %q", out.String())
	}

	released := &bytes.Buffer{}
	fake := &fakeSessions{released: 2}
	if err := runReleaseFiles(released, fake, nil, "cafe0001"); err != nil {
		t.Fatalf("release-files: %v", err)
	}
	if len(fake.patterns) != 0 {
		t.Fatalf("naming no path releases everything, got %v", fake.patterns)
	}
	if released.String() != "released 2 reservation(s)\n" {
		t.Fatalf("release-files output = %q", released.String())
	}
}
