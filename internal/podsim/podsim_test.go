// Package podsim is the local stand-in for the k8s core pod (docs/northstar/FATBABY_K8S_UDS_NORTHSTAR.md):
// it runs the REAL signalapi and newssite binaries as separate processes sharing one "emptyDir"
// socket directory and one "PVC" event store, exactly the way EMILY/gitops/specs/fatbaby-core.pod
// wires containers — and asserts the unix-socket wiring end to end. It proves socket wiring and
// notify wakeups, NOT Kubernetes scheduling. Fully isolated: every path is under one temp dir, no
// live var/, no shared secrets, signalapi has NO TCP listener at all.
//
// Run:  scripts/build-bins.sh /tmp/podbins && PODSIM_BINS=/tmp/podbins go test ./internal/podsim -v
package podsim

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/udsipc"
)

type proc struct{ cmd *exec.Cmd }

func start(t *testing.T, bin string, env []string, args ...string) *proc {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = filepath.Join(os.Getenv("PODSIM_APPDIR"))
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
	logf, _ := os.CreateTemp("", filepath.Base(bin)+"-*.log")
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill() // exact PID only — never a broad pkill
		cmd.Wait()
		if t.Failed() {
			b, _ := os.ReadFile(logf.Name())
			t.Logf("--- %s log ---\n%s", filepath.Base(bin), b)
		}
		os.Remove(logf.Name())
	})
	return &proc{cmd}
}

func waitFor(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func getJSON(t *testing.T, c *http.Client, url string) map[string]any {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: %d %s", url, resp.StatusCode, b)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("GET %s: not JSON: %s", url, b)
	}
	return m
}

func TestCorePodSocketWiring(t *testing.T) {
	bins := os.Getenv("PODSIM_BINS")
	if bins == "" {
		t.Skip("set PODSIM_BINS to a dir built by scripts/build-bins.sh")
	}
	root, err := os.MkdirTemp("", "podsim")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	runDir := filepath.Join(root, "run") // the emptyDir at /run/fatbaby
	varDir := filepath.Join(root, "var") // the RWO PVC at /app/var
	notifyDir := filepath.Join(runDir, "notify")
	store := filepath.Join(varDir, "secwatch")
	for _, d := range []string{runDir, notifyDir, store} {
		if err := os.MkdirAll(d, 0o775); err != nil {
			t.Fatal(err)
		}
	}
	// The image's /app: signalapi opens ./migrations/mysql relative to its WORKDIR, so the image must
	// COPY migrations (the first thing this simulator caught — Dockerfile fixed in the same change).
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o775); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"migrations", "config"} {
		if out, err := exec.Command("cp", "-r", filepath.Join("..", "..", d), filepath.Join(appDir, d)).CombinedOutput(); err != nil {
			t.Fatalf("copy %s: %v %s", d, err, out)
		}
	}
	env := []string{"FATBABY_NOTIFY_DIR=" + notifyDir, "HOME=" + root, "FATBABY_ROOT=" + root}
	t.Setenv("PODSIM_APPDIR", appDir)
	t.Setenv("FATBABY_NOTIFY_DIR", notifyDir) // this test process is the "secwatch" writer

	fs, err := eventstore.NewFileStore(store)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	appendEv := func(id string) {
		data := fmt.Sprintf(`{"ticker":"AAPL","signal_type":"podsim","summary":"%s","timestamp":%q}`, id, time.Now().UTC().Format(time.RFC3339))
		ev := eventstore.Event{ID: id, Type: "signal_generated", PartitionKey: "AAPL:" + id, Data: json.RawMessage(data)}
		if _, err := fs.Append(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	appendEv("e1")

	sigSock := "unix://" + filepath.Join(runDir, "signalapi.sock")
	newsSock := "unix://" + filepath.Join(runDir, "newssite.sock")
	port := freePort(t)

	// signalapi: socket ONLY (no TCP listener) — anything reaching it proves the UDS path.
	// 10-minute poll interval: only a notify wakeup can make it see e2 inside this test.
	start(t, filepath.Join(bins, "signalapi"), env, "-addr", sigSock, "-store", store,
		"-poll-interval", "10m", "-index-db", filepath.Join(varDir, "signalapi-index.db"))
	// newssite: TCP for the "Ingress" + its own socket; reaches signalapi only by socket.
	start(t, filepath.Join(bins, "newssite"), env,
		"-addr", fmt.Sprintf("127.0.0.1:%d", port), "-also-listen", newsSock, "-signalapi-url", sigSock,
		"-store", store, "-graph-dir", filepath.Join(varDir, "entity-graph"), "-eps-dir", filepath.Join(varDir, "eps"),
		"-commentary-dir", filepath.Join(varDir, "commentary"), "-guidance-dir", filepath.Join(varDir, "guidance"),
		"-earnings-cal-dir", filepath.Join(varDir, "earnings-calendar"))

	sigPath, _ := udsipc.SocketPath(sigSock)
	newsPath, _ := udsipc.SocketPath(newsSock)
	waitFor(t, "sockets", 30*time.Second, func() bool {
		_, e1 := os.Stat(sigPath)
		_, e2 := os.Stat(newsPath)
		return e1 == nil && e2 == nil
	})
	for _, p := range []string{sigPath, newsPath} {
		fi, _ := os.Stat(p)
		if fi.Mode().Perm() != udsipc.SocketMode {
			t.Errorf("%s mode %v want %v", p, fi.Mode().Perm(), udsipc.SocketMode)
		}
	}

	sig := udsipc.HTTPClient(sigSock, 3*time.Second)
	var h map[string]any
	waitFor(t, "signalapi health over its socket", 30*time.Second, func() bool {
		resp, err := sig.Get("http://unix/v1/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == 200
	})
	h = getJSON(t, sig, "http://unix/v1/health")
	if h["ok"] != true || h["latest_seq"].(float64) != 1 {
		t.Fatalf("initial health %v", h)
	}

	// 1. Cross-container proxy: the "Ingress-facing" newssite TCP port reaches signalapi's
	//    socket-only listener through its reverse proxy.
	web := &http.Client{Timeout: 5 * time.Second}
	waitFor(t, "newssite up", 30*time.Second, func() bool {
		r, err := web.Get(fmt.Sprintf("http://127.0.0.1:%d/signalapi/v1/health", port))
		if err != nil {
			return false
		}
		r.Body.Close()
		return r.StatusCode == 200
	})
	px := getJSON(t, web, fmt.Sprintf("http://127.0.0.1:%d/signalapi/v1/health", port))
	if px["ok"] != true {
		t.Fatalf("proxied health %v", px)
	}

	// 2. newssite's own socket serves the same site (what movers-watcher would POST to in-pod).
	ns := udsipc.HTTPClient(newsSock, 3*time.Second)
	r, err := ns.Get("http://unix/signalapi/v1/health")
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("newssite via its own socket: %v %v", r, err)
	}
	r.Body.Close()

	// 3. Notify: a writer in another "container" appends; signalapi (10m poll) sees it fast.
	appendEv("e2")
	start := time.Now()
	waitFor(t, "signalapi to ingest e2 via notify wakeup (poll interval is 10m)", 10*time.Second, func() bool {
		m := getJSON(t, sig, "http://unix/v1/health")
		return m["latest_seq"].(float64) == 2
	})
	t.Logf("append -> visible in signalapi in %v (poll interval 10m)", time.Since(start))
}

// TestCutoverVerifyScript runs scripts/cutover-verify.sh against two real signalapi processes
// standing in for "old (box)" and "new (pod)": a missing event must be flagged (exit 1) and a
// caught-up store must pass (exit 0) — the gate that keeps the systemd units running until the pod
// really has the same data.
func TestCutoverVerifyScript(t *testing.T) {
	bins := os.Getenv("PODSIM_BINS")
	if bins == "" {
		t.Skip("set PODSIM_BINS to a dir built by scripts/build-bins.sh")
	}
	root, _ := os.MkdirTemp("", "podsim")
	t.Cleanup(func() { os.RemoveAll(root) })
	appDir := filepath.Join(root, "app")
	os.MkdirAll(appDir, 0o775)
	for _, d := range []string{"migrations", "config"} {
		if out, err := exec.Command("cp", "-r", filepath.Join("..", "..", d), filepath.Join(appDir, d)).CombinedOutput(); err != nil {
			t.Fatalf("copy %s: %v %s", d, err, out)
		}
	}
	t.Setenv("PODSIM_APPDIR", appDir)
	t.Setenv("FATBABY_NOTIFY_DIR", "") // plain polling here; notify is covered by the other test

	mk := func(name string, ids ...string) (*eventstore.FileStore, string) {
		dir := filepath.Join(root, name, "var", "secwatch")
		os.MkdirAll(dir, 0o775)
		fs, err := eventstore.NewFileStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
		for _, id := range ids {
			data := fmt.Sprintf(`{"ticker":"AAPL","signal_type":"x","summary":"%s","timestamp":%q}`, id, time.Now().UTC().Format(time.RFC3339))
			if _, err := fs.Append(context.Background(), eventstore.Event{ID: id, Type: "signal_generated", PartitionKey: "AAPL:" + id, Data: json.RawMessage(data)}); err != nil {
				t.Fatal(err)
			}
		}
		return fs, dir
	}
	_, oldDir := mk("old", "a", "b", "c")
	newFS, newDir := mk("new", "a", "b")
	oldPort, newPort := freePort(t), freePort(t)
	env := []string{"HOME=" + root}
	start(t, filepath.Join(bins, "signalapi"), env, "-addr", fmt.Sprintf("127.0.0.1:%d", oldPort), "-store", oldDir, "-poll-interval", "200ms", "-index-db", filepath.Join(root, "old", "idx.db"))
	start(t, filepath.Join(bins, "signalapi"), env, "-addr", fmt.Sprintf("127.0.0.1:%d", newPort), "-store", newDir, "-poll-interval", "200ms", "-index-db", filepath.Join(root, "new", "idx.db"))
	oldURL, newURL := fmt.Sprintf("http://127.0.0.1:%d", oldPort), fmt.Sprintf("http://127.0.0.1:%d", newPort)
	web := &http.Client{Timeout: 2 * time.Second}
	for _, u := range []string{oldURL, newURL} {
		u := u
		waitFor(t, u, 30*time.Second, func() bool {
			r, err := web.Get(u + "/v1/health")
			if err != nil {
				return false
			}
			r.Body.Close()
			return r.StatusCode == 200
		})
	}
	run := func() (int, string) {
		out, err := exec.Command("../../scripts/cutover-verify.sh", oldURL, newURL).CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return code, string(out)
	}
	if code, out := run(); code != 1 {
		t.Fatalf("new is missing event c: want exit 1, got %d\n%s", code, out)
	}
	data := fmt.Sprintf(`{"ticker":"AAPL","signal_type":"x","summary":"c","timestamp":%q}`, time.Now().UTC().Format(time.RFC3339))
	if _, err := newFS.Append(context.Background(), eventstore.Event{ID: "c", Type: "signal_generated", PartitionKey: "AAPL:c", Data: json.RawMessage(data)}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "verify to pass once the new side catches up", 10*time.Second, func() bool {
		code, _ := run()
		return code == 0
	})
	// An unreachable side is distinguished from a data mismatch.
	out, err := exec.Command("../../scripts/cutover-verify.sh", oldURL, "http://127.0.0.1:1").CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
		t.Fatalf("unreachable side: want exit 2, got %v\n%s", err, out)
	}
}
