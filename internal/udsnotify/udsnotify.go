// Package udsnotify is a best-effort "the event store just grew" wakeup bus over unix datagram
// sockets. Writers call Publish after a successful Append; tailers that used to sleep a full
// poll interval Wait on a Subscription instead and wake immediately. Polling stays as the
// fallback, so a lost datagram, a missing directory, or a process that never subscribed costs at
// most one poll interval — never correctness (docs/northstar/FATBABY_K8S_UDS_NORTHSTAR.md).
//
// Enabled only when FATBABY_NOTIFY_DIR is set (in the k8s core pod: an emptyDir path such as
// /run/fatbaby/notify). Unset = every function is a no-op and Wait is a plain timer.
package udsnotify

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EnvDir is the environment variable naming the rendezvous directory.
const EnvDir = "FATBABY_NOTIFY_DIR"

// Dir returns the configured rendezvous directory ("" when disabled).
func Dir() string { return os.Getenv(EnvDir) }

// Publish sends a wakeup datagram to every subscriber socket in dir. Sockets whose owner is gone
// are removed. It never blocks on a slow subscriber and never returns an error: it is a hint.
func Publish(dir string) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sock") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		c, err := net.DialTimeout("unixgram", path, 50*time.Millisecond)
		if err != nil {
			// Nobody bound to it any more (crashed subscriber) — clean up the stale file.
			os.Remove(path)
			continue
		}
		c.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
		c.Write([]byte{1})
		c.Close()
	}
}

// PublishEnv is Publish(Dir()).
func PublishEnv() { Publish(Dir()) }

// Subscription receives coalesced wakeups. A nil *Subscription is valid: Wait is a plain timer.
type Subscription struct {
	conn net.PacketConn
	path string
	c    chan struct{}
	once sync.Once
}

// Subscribe binds <dir>/<name>-<pid>.sock and starts listening. Returns (nil, nil) when dir is "".
func Subscribe(dir, name string) (*Subscription, error) {
	if dir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o775); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%d.sock", name, os.Getpid()))
	os.Remove(path)
	pc, err := net.ListenPacket("unixgram", path)
	if err != nil {
		return nil, err
	}
	os.Chmod(path, 0o660)
	s := &Subscription{conn: pc, path: path, c: make(chan struct{}, 1)}
	go func() {
		buf := make([]byte, 16)
		for {
			if _, _, err := pc.ReadFrom(buf); err != nil {
				return
			}
			select {
			case s.c <- struct{}{}:
			default: // already a pending wakeup; coalesce
			}
		}
	}()
	return s, nil
}

// SubscribeEnv subscribes under Dir(). On error it returns nil (polling fallback), never fails
// the caller: a wakeup optimisation must not stop a processor from starting.
func SubscribeEnv(name string) *Subscription {
	s, err := Subscribe(Dir(), name)
	if err != nil {
		return nil
	}
	return s
}

// Wait blocks until a wakeup arrives, d elapses, or ctx is done. It returns false only when ctx
// is done. Safe on a nil Subscription (timer only).
func (s *Subscription) Wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	var ch <-chan struct{}
	if s != nil {
		ch = s.c
	}
	select {
	case <-ctx.Done():
		return false
	case <-ch:
		return true
	case <-t.C:
		return true
	}
}

// C exposes the wakeup channel for select loops (nil channel on a nil Subscription: never fires).
func (s *Subscription) C() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.c
}

// Close unbinds and removes the socket.
func (s *Subscription) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		s.conn.Close()
		os.Remove(s.path)
	})
}
