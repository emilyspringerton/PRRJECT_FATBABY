package newssite

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/udsipc"
)

func TestResolveServiceURLTCPPassthrough(t *testing.T) {
	base, dial := resolveServiceURL("http://localhost:9091")
	if base != "http://localhost:9091" || dial != "" {
		t.Fatalf("%q %q", base, dial)
	}
}

// A signalapi-shaped service on a unix socket is reachable through SetSignalapiURL's resolved
// client, and the reverse-proxy path (proxySignalAPI) forwards over the same socket.
func TestSignalapiOverUnixSocket(t *testing.T) {
	dir, _ := os.MkdirTemp("", "uds")
	defer os.RemoveAll(dir)
	addr := "unix://" + filepath.Join(dir, "signalapi.sock")
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ping", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "pong") })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go udsipc.ServeHTTP(ctx, addr, mux)
	p, _ := udsipc.SocketPath(addr)
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(p); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	h := &Handler{}
	h.SetSignalapiURL(addr)
	if h.signalapiURL != "http://unix" || h.signalapiDial != addr {
		t.Fatalf("resolved %q %q", h.signalapiURL, h.signalapiDial)
	}
	c := serviceClient(h.signalapiDial, 2*time.Second)
	resp, err := c.Get(h.signalapiURL + "/v1/ping")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "pong" {
		t.Fatalf("direct: %q", b)
	}

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/signalapi/v1/ping", nil)
	if st := h.proxySignalAPI(rec, req); st != 200 || rec.Body.String() != "pong" {
		t.Fatalf("proxy: status %d body %q", st, rec.Body.String())
	}
}
