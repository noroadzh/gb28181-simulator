// End-to-end coverage for the runtime node lifecycle.
//
// Change: runtime-node-configuration, task 8.1.
//
// Unlike the in-process smoke probes in this package, this test drives the
// real binary: it builds cmd/gb28181-simulator, boots it over a throwaway
// database, and talks to it over HTTP. That is the only way to assert what
// task 8.1 asks for — that a node created through the API actually holds its
// SIP socket while it runs, and hands the port back once it is deleted. The
// listener lives in another process, so nothing short of a real bind can
// prove it.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Node ids used by the runtime CRUD probes. The device id's type code (131)
// is the one model.CheckNodeKindConsistency expects for `device`.
const (
	e2eDeviceID = "34020000001310000001"
	e2eLargeID  = "34020000002000000001"
	e2eSmallID  = "34020000002160000001"
)

// buildSimulator compiles the operator binary once per test into its temp
// directory. `go build` is cheap after the first call (the build cache is
// shared), and building per test keeps cleanup automatic.
func buildSimulator(t *testing.T) string {
	t.Helper()
	name := "gb28181-simulator"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/gb28181-simulator")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/gb28181-simulator: %v\n%s", err, out)
	}
	return bin
}

// repoRoot walks up from this file to the module root so the test can build
// the package regardless of the directory `go test` was invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller: cannot locate this test file")
	}
	// <root>/internal/test/e2e/<this file>
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

// freeUDPPort reserves a loopback UDP port and releases it, leaving the
// number for the simulator to bind. The window between close and bind is a
// race in principle; on loopback at test scale it is not one in practice.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve udp port: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	if err := pc.Close(); err != nil {
		t.Fatalf("release udp port: %v", err)
	}
	return port
}

// freeTCPPort is freeUDPPort for the HTTP listener.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve tcp port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("release tcp port: %v", err)
	}
	return port
}

// bindUDP reports whether addr is free by trying to take it. A nil error
// means the port is available; the caller must close the returned connection.
func bindUDP(addr string) (net.PacketConn, error) {
	return net.ListenPacket("udp", addr)
}

// writeSimulatorConfig renders the config the e2e fleet boots with: the store
// and logs live in the test directory, capture is on (the cascade test reads
// the device's inbound wire), tracing stays off to keep stdout quiet.
func writeSimulatorConfig(t *testing.T, dir string, httpPort int) string {
	t.Helper()
	return writeSimulatorConfigWith(t, filepath.Join(dir, "config.yaml"), dir, httpPort, false)
}

// writeSimulatorConfigWith renders a config at an explicit path. autoStart
// writes the top-level `auto_start` switch the boot path reads.
func writeSimulatorConfigWith(t *testing.T, path, dir string, httpPort int, autoStart bool) string {
	t.Helper()
	auto := ""
	if autoStart {
		auto = "auto_start: true\n"
	}
	cfg := fmt.Sprintf(`http:
  host: 127.0.0.1
  port: %d
log:
  level: error
  file: %q
storage:
  path: %q
capture:
  enabled: true
  capacity: 256
tracing:
  enabled: false
%s`, httpPort,
		filepath.Join(dir, "app.log"),
		filepath.Join(dir, "data.db"),
		auto)
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// simulatorProcess is one running binary plus the helpers to drive it.
type simulatorProcess struct {
	baseURL string
	cmd     *exec.Cmd
	logPath string
	appLog  string
	logFile *os.File
	done    chan struct{}
	wait    sync.Once
}

func startSimulator(t *testing.T, bin, cfgPath, dir string, httpPort int) *simulatorProcess {
	t.Helper()
	// The log is named after the config so a test that boots twice over the
	// same directory keeps both processes' output.
	logPath := filepath.Join(dir, filepath.Base(cfgPath)+".process.log")
	appLog := filepath.Join(dir, "app.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create process log: %v", err)
	}

	cmd := exec.Command(bin, "-config", cfgPath)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start simulator: %v", err)
	}

	p := &simulatorProcess{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", httpPort),
		cmd:     cmd,
		logPath: logPath,
		appLog:  appLog,
		logFile: logFile,
		done:    make(chan struct{}),
	}
	t.Cleanup(func() {
		p.kill()
		if t.Failed() {
			p.dumpLog(t)
		}
		_ = logFile.Close()
	})
	p.waitReady(t)
	return p
}

// reap closes done once the process has exited, whoever waits first.
func (p *simulatorProcess) reap() {
	p.wait.Do(func() {
		_ = p.cmd.Wait()
		close(p.done)
	})
}

func (p *simulatorProcess) kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	p.reap()
}

// stop shuts the process down the way an operator would — SIGINT, then a
// grace period — so a test can restart it over the same database.
func (p *simulatorProcess) stop(t *testing.T) {
	t.Helper()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(os.Interrupt)
	}
	go p.reap()
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

// dumpLog prints the last few kilobytes of the process output on failure.
func (p *simulatorProcess) dumpLog(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(p.appLog)
	if err != nil {
		raw, err = os.ReadFile(p.logPath)
	}
	if err != nil {
		t.Logf("read process log: %v", err)
		return
	}
	const tail = 12000
	if len(raw) > tail {
		raw = raw[len(raw)-tail:]
	}
	t.Logf("simulator process log (tail):\n%s", raw)
}

// waitReady polls /healthz until the HTTP server is up.
func (p *simulatorProcess) waitReady(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(p.baseURL + "/healthz")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("simulator did not answer /healthz within 20s at %s", p.baseURL)
}

// call issues one JSON request and returns the status plus the decoded body.
func (p *simulatorProcess) call(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s %s body: %v", method, path, err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, p.baseURL+path, rdr)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s body: %v", method, path, err)
	}
	var decoded map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s %s returned non-JSON %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode, decoded
}

// waitNodeStatus polls GET /v1/nodes/{id} until the node reports want.
func (p *simulatorProcess) waitNodeStatus(t *testing.T, id, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	last := "<never read>"
	for time.Now().Before(deadline) {
		status, body := p.call(t, http.MethodGet, "/v1/nodes/"+id, nil)
		if status == http.StatusOK {
			if got, _ := body["status"].(string); got == want {
				return
			} else {
				last = got
			}
		} else {
			last = fmt.Sprintf("HTTP %d", status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("node %s never reached status %q (last saw %q)", id, want, last)
}

// TestRuntimeNodeCRUD_BindsAndReleasesSIPPort is task 8.1: create → start →
// the SIP port is really held → delete is refused while live → stop →
// delete → the port is free again.
func TestRuntimeNodeCRUD_BindsAndReleasesSIPPort(t *testing.T) {
	bin := buildSimulator(t)

	dir := t.TempDir()
	httpPort := freeTCPPort(t)
	sipAddr := fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))
	cfgPath := writeSimulatorConfig(t, dir, httpPort)
	proc := startSimulator(t, bin, cfgPath, dir, httpPort)

	// --- create -------------------------------------------------------
	status, created := proc.call(t, http.MethodPost, "/v1/nodes", map[string]any{
		"id":     e2eDeviceID,
		"kind":   "device",
		"domain": "3402000000",
		"addr":   sipAddr,
		"vendor": "e2e",
	})
	if status != http.StatusCreated {
		t.Fatalf("POST /v1/nodes = %d, want 201 (body %v)", status, created)
	}
	if got, _ := created["id"].(string); got != e2eDeviceID {
		t.Fatalf("created id = %q, want %q", got, e2eDeviceID)
	}

	// --- start --------------------------------------------------------
	status, started := proc.call(t, http.MethodPost, "/v1/nodes/"+e2eDeviceID+"/start", nil)
	if status != http.StatusOK {
		t.Fatalf("POST start = %d, want 200 (body %v)", status, started)
	}
	proc.waitNodeStatus(t, e2eDeviceID, "online")

	// A device without an upstream registration goes online as soon as its
	// listener is bound, so "online" must mean the socket is really held.
	if conn, err := bindUDP(sipAddr); err == nil {
		_ = conn.Close()
		t.Fatalf("SIP endpoint %s was free while node %s reported online: the node does not hold its listener", sipAddr, e2eDeviceID)
	}

	// --- deleting a live node is refused ------------------------------
	status, refused := proc.call(t, http.MethodDelete, "/v1/nodes/"+e2eDeviceID, nil)
	if status != http.StatusConflict {
		t.Fatalf("DELETE while online = %d, want 409 (body %v)", status, refused)
	}

	// --- stop ---------------------------------------------------------
	status, stopped := proc.call(t, http.MethodPost, "/v1/nodes/"+e2eDeviceID+"/stop", nil)
	if status != http.StatusOK {
		t.Fatalf("POST stop = %d, want 200 (body %v)", status, stopped)
	}
	proc.waitNodeStatus(t, e2eDeviceID, "offline")

	// --- delete -------------------------------------------------------
	status, deleted := proc.call(t, http.MethodDelete, "/v1/nodes/"+e2eDeviceID, nil)
	if status != http.StatusOK {
		t.Fatalf("DELETE after stop = %d, want 200 (body %v)", status, deleted)
	}
	if got, _ := deleted["status"].(string); got != "deleted" {
		t.Errorf("delete response status = %q, want deleted", got)
	}
	if status, _ := proc.call(t, http.MethodGet, "/v1/nodes/"+e2eDeviceID, nil); status != http.StatusNotFound {
		t.Errorf("GET a deleted node = %d, want 404", status)
	}

	// --- the port is back --------------------------------------------
	conn, err := bindUDP(sipAddr)
	if err != nil {
		t.Fatalf("SIP endpoint %s still held after deleting node %s: %v", sipAddr, e2eDeviceID, err)
	}
	if closeErr := conn.Close(); closeErr != nil {
		t.Fatalf("close probe socket: %v", closeErr)
	}
}
