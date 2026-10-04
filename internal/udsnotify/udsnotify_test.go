package udsnotify

import (
	"context"
	"os"
	"testing"
	"time"
)

func tmp(t *testing.T) string {
	d, _ := os.MkdirTemp("", "un")
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func TestPublishWakesWaitFast(t *testing.T) {
	dir := tmp(t)
	s, err := Subscribe(dir, "proc")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	go func() { time.Sleep(50 * time.Millisecond); Publish(dir) }()
	start := time.Now()
	if !s.Wait(context.Background(), 10*time.Second) {
		t.Fatal("ctx not done but Wait false")
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("woke after %v, want ~50ms", el)
	}
}

func TestBurstCoalescesAndAllSubscribersWake(t *testing.T) {
	dir := tmp(t)
	a, _ := Subscribe(dir, "a")
	defer a.Close()
	b, _ := Subscribe(dir, "b")
	defer b.Close()
	for i := 0; i < 50; i++ {
		Publish(dir)
	}
	time.Sleep(100 * time.Millisecond)
	for _, s := range []*Subscription{a, b} {
		select {
		case <-s.C():
		default:
			t.Fatal("subscriber missed wakeup")
		}
		select {
		case <-s.C():
			t.Fatal("burst was not coalesced")
		default:
		}
	}
}

func TestNilAndDisabledFallBackToTimer(t *testing.T) {
	var s *Subscription
	start := time.Now()
	if !s.Wait(context.Background(), 60*time.Millisecond) || time.Since(start) < 50*time.Millisecond {
		t.Fatal("nil subscription must behave as a timer")
	}
	Publish("") // no-op
	if sub, err := Subscribe("", "x"); sub != nil || err != nil {
		t.Fatal("empty dir must disable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.Wait(ctx, time.Hour) {
		t.Fatal("cancelled ctx must return false")
	}
}

func TestPublishRemovesStaleSocketAndMissingDirIsHarmless(t *testing.T) {
	dir := tmp(t)
	s, _ := Subscribe(dir, "dead")
	// Simulate a crash: close the conn without removing the file.
	s.conn.Close()
	Publish(dir)
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("stale socket should have been cleaned")
	}
	Publish("/nonexistent/dir/for/test")
}
