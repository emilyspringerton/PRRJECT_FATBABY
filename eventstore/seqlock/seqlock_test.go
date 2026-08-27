package seqlock

import (
	"os"
	"testing"
	"time"
)

func TestAcquireRelease(t *testing.T) {
	f, err := os.CreateTemp("", "seqlock-test-*.lock")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	fd, ok := Acquire(path)
	if !ok {
		t.Fatal("acquire failed")
	}
	if !Release(fd) {
		t.Fatal("release failed")
	}
}

// TestMutualExclusion proves the lock is real: multiple goroutines racing
// to enter a critical section, each protected by Acquire/Release around a
// real sleep to widen the race window. Without a real lock, two
// goroutines would interleave and the shared flag check below would
// catch it; with it, exactly one goroutine is ever inside at a time.
func TestMutualExclusion(t *testing.T) {
	f, err := os.CreateTemp("", "seqlock-mutex-test-*.lock")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	const goroutines = 8
	const perGoroutine = 20
	results := make(chan bool, goroutines*perGoroutine)
	inCritical := int32(0)

	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < perGoroutine; i++ {
				fd, ok := Acquire(path)
				if !ok {
					results <- false
					continue
				}
				if inCritical != 0 {
					results <- false
					Release(fd)
					continue
				}
				inCritical = 1
				time.Sleep(2 * time.Millisecond)
				inCritical = 0
				results <- Release(fd)
			}
		}()
	}

	for i := 0; i < goroutines*perGoroutine; i++ {
		if !<-results {
			t.Fatal("mutual exclusion violated or acquire/release failed")
		}
	}
}
