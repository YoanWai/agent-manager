package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Agents reserving the same path at the same moment are the collision the
// lease exists to report. Each holder opens its own handle on the shared
// database, the way separate processes do, so a conflict read that ran
// before someone else's write would leave both of them believing the path
// was theirs alone.
func TestConcurrentReservationsLeaveOnlyTheFirstHolderUnaware(t *testing.T) {
	const holders, rounds = 8, 20
	path := filepath.Join(t.TempDir(), "test.db")
	stores := make([]*Store, holders)
	for index := range stores {
		st, err := Open(path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { st.Close() })
		stores[index] = st
	}

	for round := range rounds {
		pattern := fmt.Sprintf("internal/store/round%d.go", round)
		now := time.Now()
		clashCounts := make([]int, holders)
		failures := make([]error, holders)
		start := make(chan struct{})
		var ready, running sync.WaitGroup
		for index := range stores {
			ready.Add(1)
			running.Add(1)
			go func(index int) {
				defer running.Done()
				ready.Done()
				<-start
				clashes, err := stores[index].Reserve(Reservation{
					ID:         fmt.Sprintf("r%d-%d", round, index),
					SessionID:  fmt.Sprintf("s%d", index),
					Pattern:    pattern,
					Mode:       ReservationExclusive,
					AcquiredAt: now,
					ExpiresAt:  now.Add(time.Hour),
				})
				clashCounts[index], failures[index] = len(clashes), err
			}(index)
		}
		ready.Wait()
		close(start)
		running.Wait()

		unaware := 0
		for index, err := range failures {
			if err != nil {
				t.Fatalf("holder %d: %v", index, err)
			}
			if clashCounts[index] == 0 {
				unaware++
			}
		}
		if unaware != 1 {
			t.Fatalf("round %d: %d of %d holders were never told about the clash; only the one that got there first may miss it",
				round, unaware, holders)
		}
	}
}
