package mangadex

import (
	"context"
	"testing"
	"time"
)

// TestWaitSpacesConsecutiveCalls covers the requests/second budget the slot
// accounting protects. Each iteration stands in for one request: wait for a
// slot, then spend longer than a slot on the request itself, which is what makes
// an under-reserved slot let requests fire back to back.
func TestWaitSpacesConsecutiveCalls(t *testing.T) {
	client, err := NewClient("http://example.com", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	const simulatedLatency = 200 * time.Millisecond

	var fired []time.Time

	for range 3 {
		if err := client.wait(context.Background()); err != nil {
			t.Fatalf("wait: %v", err)
		}

		fired = append(fired, time.Now())
		time.Sleep(simulatedLatency)
	}

	// Timer wake-up jitter eats a little of each gap, so allow a small slack: an
	// under-reserved slot shows up as a gap near the request latency instead.
	tolerance := 20 * time.Millisecond

	for i := 1; i < len(fired); i++ {
		if gap := fired[i].Sub(fired[i-1]); gap < requestInterval-tolerance {
			t.Fatalf("requests fired %s apart, want at least %s", gap, requestInterval-tolerance)
		}
	}
}
