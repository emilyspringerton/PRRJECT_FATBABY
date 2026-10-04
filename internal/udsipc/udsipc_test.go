package udsipc

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sockAddr(t *testing.T) string {
	// t.TempDir paths can exceed the 108-byte sun_path limit; use a short dir.
	d, err := os.MkdirTemp("", "uds")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return "unix://" + filepath.Join(d, "a.sock")
}

func TestSocketPathValidation(t *testing.T) {
	for _, bad := range []string{"127.0.0.1:1", "unix://", "unix://rel.sock"} {
		if _, err := SocketPath(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	if p, err := SocketPath("unix:///run/x/../fatbaby/a.sock"); err != nil || p != "/run/fatbaby/a.sock" {
		t.Errorf("got %q %v", p, err)
	}
}

func TestHTTPOverUnixRoundTripAndCleanup(t *testing.T) {
	addr := sockAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hi") })
	errc := make(chan error, 1)
	go func() { errc <- ServeHTTP(ctx, addr, mux) }()
	p, _ := SocketPath(addr)
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(p); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != SocketMode {
		t.Fatalf("socket stat %v mode %v", err, fi)
	}
	c, u, err := ClientFor(addr+":/hello", time.Second)
	if err != nil || u != "http://unix/hello" {
		t.Fatalf("ClientFor %q %v", u, err)
	}
	resp, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "hi" {
		t.Fatalf("body %q", b)
	}
	cancel()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("socket not removed: %v", err)
	}
}

func TestStaleSocketReplacedLiveOneRefused(t *testing.T) {
	addr := sockAddr(t)
	p, _ := SocketPath(addr)
	// Create a stale socket file: listen then close without unlinking.
	l, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	l2, err := Listen(addr)
	if err != nil {
		t.Fatalf("stale socket should be replaced: %v", err)
	}
	if _, err := Listen(addr); err == nil {
		t.Fatal("second Listen on a live socket must fail")
	}
	l2.Close()
}

func TestRefusesToRemoveNonSocket(t *testing.T) {
	addr := sockAddr(t)
	p, _ := SocketPath(addr)
	if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(addr); err == nil {
		t.Fatal("must not delete a regular file")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("file was removed")
	}
}

func TestPeerCred(t *testing.T) {
	addr := sockAddr(t)
	l, err := Listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	got := make(chan uint32, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			got <- 1 << 31
			return
		}
		defer c.Close()
		uid, _, pid, err := PeerCred(c)
		if err != nil || int(pid) != os.Getpid() {
			got <- 1 << 31
			return
		}
		got <- uid
	}()
	c, err := Dial(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if uid := <-got; uid != uint32(os.Getuid()) {
		t.Fatalf("peer uid %d want %d", uid, os.Getuid())
	}
}

func TestParseURLTCPPassthrough(t *testing.T) {
	base, dial, err := ParseURL("http://localhost:8082/api/commentary")
	if err != nil || base != "http://localhost:8082/api/commentary" || dial != "localhost:8082" {
		t.Fatalf("%q %q %v", base, dial, err)
	}
}
