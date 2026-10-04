// Package udsipc is FatBaby's same-pod / same-node IPC layer: unix domain sockets instead of
// loopback TCP. In Kubernetes only containers sharing a volume (an emptyDir mounted at
// /run/fatbaby) can reach each other this way, which is exactly the "processors in the core pod"
// boundary (docs/northstar/FATBABY_K8S_UDS_NORTHSTAR.md).
//
// Addresses are strings: "unix:///run/fatbaby/signalapi.sock" is a socket, anything else
// ("127.0.0.1:9091", ":8082") is plain TCP, so every listener/dialer in the pipeline works
// unchanged on the systemd box and in the pod.
package udsipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const unixPrefix = "unix://"

// SocketMode is the permission applied to every socket file we create.
const SocketMode os.FileMode = 0o660

// IsUnix reports whether addr names a unix socket ("unix:///path").
func IsUnix(addr string) bool { return strings.HasPrefix(addr, unixPrefix) }

// SocketPath returns the filesystem path of a unix address.
func SocketPath(addr string) (string, error) {
	if !IsUnix(addr) {
		return "", fmt.Errorf("udsipc: %q is not a unix:// address", addr)
	}
	p := strings.TrimPrefix(addr, unixPrefix)
	if p == "" || !filepath.IsAbs(p) {
		return "", fmt.Errorf("udsipc: unix address %q needs an absolute path (unix:///run/x.sock)", addr)
	}
	return filepath.Clean(p), nil
}

// Listen opens a listener for addr. For unix addresses it creates the parent directory, removes
// a stale socket file left by a crashed process (but refuses if something still answers on it),
// and applies SocketMode.
func Listen(addr string) (net.Listener, error) {
	if !IsUnix(addr) {
		return net.Listen("tcp", addr)
	}
	path, err := SocketPath(addr)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o775); err != nil {
		return nil, fmt.Errorf("udsipc: mkdir socket dir: %w", err)
	}
	if err := removeStale(path); err != nil {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, SocketMode); err != nil {
		l.Close()
		return nil, fmt.Errorf("udsipc: chmod socket: %w", err)
	}
	return l, nil
}

// removeStale deletes path if it is a socket nobody is listening on.
func removeStale(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("udsipc: %s exists and is not a socket; refusing to remove it", path)
	}
	c, err := net.DialTimeout("unix", path, 250*time.Millisecond)
	if err == nil {
		c.Close()
		return fmt.Errorf("udsipc: %s is already in use by a live process", path)
	}
	return os.Remove(path)
}

// Dial connects to addr (unix:// or TCP host:port).
func Dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	if IsUnix(addr) {
		p, err := SocketPath(addr)
		if err != nil {
			return nil, err
		}
		return d.DialContext(ctx, "unix", p)
	}
	return d.DialContext(ctx, "tcp", addr)
}

// HTTPClient returns an http.Client that sends every request to addr, whatever host the URL
// names (use "http://signalapi/..." style URLs; the host is only a label).
func HTTPClient(addr string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return Dial(ctx, addr)
			},
			MaxIdleConns:    4,
			IdleConnTimeout: 30 * time.Second,
		},
	}
}

// ParseURL splits a service URL into (base URL for requests, dial address). It accepts:
//
//	http://127.0.0.1:9091/x             -> as is, dial TCP
//	unix:///run/fatbaby/api.sock        -> base "http://unix", dial the socket
//	unix:///run/fatbaby/api.sock:/x/y   -> same, plus path "/x/y" appended to base
//
// The ":/path" suffix is how a full endpoint URL is carried in one string (flags like
// -commentary-url). The split is at the first ".sock:/" so socket paths may contain colons only
// before that marker.
func ParseURL(raw string) (base string, dialAddr string, err error) {
	if !IsUnix(raw) {
		u, e := url.Parse(raw)
		if e != nil {
			return "", "", e
		}
		return raw, u.Host, nil
	}
	sockPart, path := raw, ""
	if i := strings.Index(raw, ".sock:/"); i >= 0 {
		sockPart, path = raw[:i+len(".sock")], raw[i+len(".sock")+1:]
	}
	if _, e := SocketPath(sockPart); e != nil {
		return "", "", e
	}
	return "http://unix" + path, sockPart, nil
}

// ClientFor returns an HTTP client + request URL for any service URL accepted by ParseURL.
// For plain http(s) URLs the returned client is a normal one.
func ClientFor(raw string, timeout time.Duration) (*http.Client, string, error) {
	base, dial, err := ParseURL(raw)
	if err != nil {
		return nil, "", err
	}
	if !IsUnix(dial) {
		return &http.Client{Timeout: timeout}, base, nil
	}
	return HTTPClient(dial, timeout), base, nil
}

// ServeHTTP serves h on addr until ctx is cancelled, then shuts down gracefully and removes the
// socket file.
func ServeHTTP(ctx context.Context, addr string, h http.Handler) error {
	l, err := Listen(addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sc)
		close(done)
	}()
	err = srv.Serve(l)
	if errors.Is(err, http.ErrServerClosed) {
		<-done
		err = nil
	}
	if IsUnix(addr) {
		if p, e := SocketPath(addr); e == nil {
			os.Remove(p)
		}
	}
	return err
}

// PeerCred returns the uid/gid/pid of the process on the other end of a unix connection.
func PeerCred(c net.Conn) (uid, gid uint32, pid int32, err error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, 0, 0, errors.New("udsipc: not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, 0, 0, err
	}
	var cred *syscall.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, serr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, 0, 0, err
	}
	if serr != nil {
		return 0, 0, 0, serr
	}
	return cred.Uid, cred.Gid, cred.Pid, nil
}
