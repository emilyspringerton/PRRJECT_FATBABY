package eventstore

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/udsnotify"
)

// An Append in one "container" wakes a tailer subscribed in another within milliseconds, far
// inside the 10s poll interval the tailer asked for.
func TestAppendWakesSubscribedTailer(t *testing.T) {
	nd, _ := os.MkdirTemp("", "nd")
	defer os.RemoveAll(nd)
	t.Setenv(udsnotify.EnvDir, nd)
	sub := udsnotify.SubscribeEnv("tailer")
	defer sub.Close()

	s, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.Append(context.Background(), Event{ID: "n1", Type: "filing_discovered", Data: json.RawMessage(`{}`)})
	}()
	start := time.Now()
	sub.Wait(context.Background(), 10*time.Second)
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("tailer woke after %v; notify did not fire", el)
	}
}

func TestAppendWithoutNotifyDirStillWorks(t *testing.T) {
	t.Setenv(udsnotify.EnvDir, "")
	s, _ := NewFileStore(t.TempDir())
	defer s.Close()
	if _, err := s.Append(context.Background(), Event{ID: "n2", Type: "filing_discovered", Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
}
