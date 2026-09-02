package main

import (
	"math/rand"
	"testing"
	"time"
)

func TestJitteredInterval_ZeroJitterIsExact(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	got := jitteredInterval(15*time.Second, 0, rng)
	if got != 15*time.Second {
		t.Errorf("jitter=0 should return base unchanged, got %v", got)
	}
}

func TestJitteredInterval_StaysWithinBounds(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	base, jitter := 30*time.Second, 10*time.Second
	for i := 0; i < 500; i++ {
		got := jitteredInterval(base, jitter, rng)
		if got < base-jitter || got > base+jitter {
			t.Fatalf("iteration %d: got %v, want within [%v,%v]", i, got, base-jitter, base+jitter)
		}
		if got <= 0 {
			t.Fatalf("iteration %d: got non-positive wait %v", i, got)
		}
	}
}

func TestJitteredInterval_NeverNegativeEvenWhenJitterExceedsBase(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	base, jitter := 5*time.Second, 10*time.Second // jitter > base
	for i := 0; i < 500; i++ {
		if got := jitteredInterval(base, jitter, rng); got <= 0 {
			t.Fatalf("iteration %d: got non-positive wait %v", i, got)
		}
	}
}

// TestNextWait_RecommendedBusinessWireValues -- the exact real values named
// for an eventual BusinessWire runner (founder, 2026-09-02): "crawl and then
// rest between 15 seconds and 1 minute +-10 and then crawl again."
func TestNextWait_RecommendedBusinessWireValues(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	min, max, jitter := 15*time.Second, time.Minute, 10*time.Second
	lo, hi := min-jitter, max+jitter
	for i := 0; i < 1000; i++ {
		got := nextWait(0, min, max, jitter, rng)
		if got < lo || got > hi {
			t.Fatalf("iteration %d: got %v, want within [%v,%v]", i, got, lo, hi)
		}
	}
}

func TestNextWait_UnsetRangeFallsBackToFixedInterval(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	got := nextWait(15*time.Second, 0, 0, 0, rng)
	if got != 15*time.Second {
		t.Errorf("with no range/jitter set, want the exact fixed interval unchanged, got %v", got)
	}
}
