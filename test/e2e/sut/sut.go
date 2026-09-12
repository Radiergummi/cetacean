//go:build e2e

// Package sut starts and supervises the real cetacean binary. Tests drive the
// process the way an operator would: an explicit environment, a port, and
// whatever it chooses to serve.
package sut

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	readyTimeout = 30 * time.Second
	exitTimeout  = 30 * time.Second
	pollInterval = 200 * time.Millisecond

	// maxLaunchAttempts bounds retries of a launch that fails with EADDRINUSE
	// during startup. waitPortFree already proves the port bindable before
	// the child starts, so this only covers the residual race between that
	// probe's close and the child's own bind.
	maxLaunchAttempts = 3
)

// Config describes one run of the binary.
type Config struct {
	// Port the binary listens on. Fixed per lane, because the proxy
	// containers' configuration names it statically.
	Port int

	// DockerHost is passed as CETACEAN_DOCKER_HOST.
	DockerHost string

	// Env is the complete set of extra variables. Nothing is inherited.
	Env map[string]string

	// TLS makes the base URL https. Set when the case configures
	// CETACEAN_TLS_CERT and CETACEAN_TLS_KEY.
	TLS bool

	// ClientCert and ClientKey, when set, are presented by Client().
	ClientCert string
	ClientKey  string

	// CACert, when set, is the root Client() trusts.
	CACert string
}

// readyPath is where this configuration's readiness probe lives. Everything
// the binary serves moves under CETACEAN_BASE_PATH, /-/ready included, so a
// probe at the root would poll a 404 until it timed out.
func (c Config) readyPath() string {
	return c.Env["CETACEAN_BASE_PATH"] + "/-/ready"
}

// Process is a running binary.
type Process struct {
	BaseURL string

	readyPath string

	cmd    *exec.Cmd
	output *lockedBuffer
	client *http.Client

	exited   chan struct{} // closed once the child has been reaped
	waitOnce sync.Once
	waitErr  error
	stopOnce sync.Once

	// killed records that Stop escalated to SIGKILL, whose exit status says
	// nothing about how the child was going to exit.
	killed bool

	// adopted marks the attempt Start handed back. An abandoned retry exits
	// non-zero by definition, and must not be asserted over.
	adopted bool
}

// reap waits for the child exactly once. It is the ONLY caller of cmd.Wait in
// this package: Wait populates cmd.ProcessState and awaits the goroutines
// copying stdout/stderr into output, so both the exit code and the full log
// are available once it returns. Safe to call from several goroutines.
func (p *Process) reap() error {
	p.waitOnce.Do(func() { p.waitErr = p.cmd.Wait() })

	return p.waitErr
}

func Start(t *testing.T, cfg Config) *Process {
	t.Helper()

	for attempt := 1; ; attempt++ {
		proc := launch(t, cfg)

		t.Cleanup(func() {
			proc.Stop()

			if proc.adopted {
				proc.assertExitedCleanly(t)
			}
		})

		err := proc.waitReady()
		if err == nil {
			proc.adopted = true

			return proc
		}

		// waitPortFree already proved the port bindable, so a bind failure
		// here is the small race between that probe's close and the child's
		// own bind. Any other startup failure fails immediately.
		if attempt < maxLaunchAttempts && addrInUseOutput(proc.Logs()) {
			continue
		}

		t.Fatalf("%v\n--- binary output ---\n%s", err, proc.Logs())
	}
}

// StartExpectingExit starts the binary and waits for it to exit by itself.
// A configuration the binary refuses is an outcome under test, not a harness
// failure — it is how startup validation gets asserted at all.
func StartExpectingExit(t *testing.T, cfg Config) (int, string) {
	t.Helper()

	for attempt := 1; ; attempt++ {
		proc := launch(t, cfg)

		select {
		case <-proc.exited:
			// cmd.ProcessState is populated whether the child exited cleanly or
			// not, so ExitCode() alone reports the outcome; waitErr is only
			// examined to distinguish a genuine Wait failure (not an
			// *exec.ExitError) from an ordinary non-zero exit.
			if proc.waitErr != nil {
				var exitErr *exec.ExitError
				if !errors.As(proc.waitErr, &exitErr) {
					t.Fatalf("Wait: %v", proc.waitErr)
				}
			}

			// See Start: a bind failure this soon after waitPortFree's probe
			// is the residual close/bind race, not the config refusal this
			// helper exists to observe. Retry it, bounded, before failing.
			if addrInUseOutput(proc.Logs()) {
				if attempt < maxLaunchAttempts {
					continue
				}

				t.Fatalf(
					"binary could not bind port %d after %d attempts\n--- binary output ---\n%s",
					cfg.Port, attempt, proc.Logs(),
				)
			}

			return proc.cmd.ProcessState.ExitCode(), proc.Logs()

		case <-time.After(exitTimeout):
			proc.Stop()
			t.Fatalf(
				"binary still running after %s; expected it to refuse\n%s",
				exitTimeout,
				proc.Logs(),
			)

			return 0, ""
		}
	}
}

func launch(t *testing.T, cfg Config) *Process {
	t.Helper()

	if cfg.DockerHost == "" {
		t.Fatal("sut: Config.DockerHost is empty; refusing to fall back to the host Docker socket")
	}

	bin := binaryPath(t)

	// Tests in a package reuse one port serially and the previous SUT's
	// listener can outlive its SIGINT. Without this wait the suite gains a
	// flaky "address in use" class that reads like a product bug.
	waitPortFree(t, cfg.Port)

	out := &lockedBuffer{}

	cmd := exec.CommandContext(context.Background(), bin)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = buildEnv(cfg)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}

	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}

	proc := &Process{
		BaseURL:   fmt.Sprintf("%s://127.0.0.1:%d", scheme, cfg.Port),
		readyPath: cfg.readyPath(),
		cmd:       cmd,
		output:    out,
		client:    buildClient(t, cfg),
		exited:    make(chan struct{}),
	}

	go func() {
		_ = proc.reap()
		close(proc.exited)
	}()

	return proc
}

// buildEnv produces the child's complete environment. PATH is carried because
// the binary shells out for nothing but needs a sane default; everything else
// is what the case asked for.
func buildEnv(cfg Config) []string {
	env := map[string]string{
		"PATH":                 os.Getenv("PATH"),
		"HOME":                 os.Getenv("HOME"),
		"CETACEAN_LISTEN_ADDR": fmt.Sprintf(":%d", cfg.Port),
		"CETACEAN_DOCKER_HOST": cfg.DockerHost,
		"CETACEAN_LOG_FORMAT":  "json",
		"CETACEAN_LOG_LEVEL":   "debug",
		"CETACEAN_SNAPSHOT":    "false",
		// A developer's own cetacean.toml (discovered via
		// config.DiscoverConfigFile when this is unset) must never leak into
		// the child: it would silently supply operations_level, acl policy,
		// prometheus.url and more, out of a case's control.
		"CETACEAN_CONFIG": "/dev/null",
	}

	// A `go build -cover` binary writes its profile to GOCOVERDIR on exit.
	// This environment is total, so the variable has to be carried explicitly —
	// and only when the parent sets it, since an uninstrumented binary would
	// otherwise try to write a profile.
	if dir := os.Getenv("GOCOVERDIR"); dir != "" {
		env["GOCOVERDIR"] = dir
	}

	// Same reasoning for GORACE: `make test-stack-race` sets halt_on_error=1
	// so the first race kills the SUT rather than being logged and outlived.
	if opts := os.Getenv("GORACE"); opts != "" {
		env["GORACE"] = opts
	}

	maps.Copy(env, cfg.Env)

	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}

	return out
}

func buildClient(t *testing.T, cfg Config) *http.Client {
	t.Helper()

	tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test client, self-signed chain

	if cfg.CACert != "" {
		pem, err := os.ReadFile(cfg.CACert)
		if err != nil {
			t.Fatalf("read CA: %v", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			t.Fatalf("CA %s is not a valid PEM bundle", cfg.CACert)
		}

		tlsCfg.RootCAs = pool
		tlsCfg.InsecureSkipVerify = false
	}

	if cfg.ClientCert != "" {
		pair, err := tls.LoadX509KeyPair(cfg.ClientCert, cfg.ClientKey)
		if err != nil {
			t.Fatalf("load client keypair: %v", err)
		}

		tlsCfg.Certificates = []tls.Certificate{pair}
	}

	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (p *Process) Client() *http.Client { return p.client }

// StreamClient is Client without the overall timeout, for a response a case
// intends to hold open — an SSE subscription, a log tail. http.Client.Timeout
// covers reading the response body too, so Client's 30s cap would tear a
// stream down mid-test. The request's own context is the bound on these.
func (p *Process) StreamClient() *http.Client {
	streaming := *p.client
	streaming.Timeout = 0

	return &streaming
}

// get issues a GET against the running binary. It exists only so waitReady's
// polling loop can build a request rather than call the context-less
// (*http.Client).Get; the client's own Timeout still bounds each attempt.
func (p *Process) get(url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	return p.client.Do(req)
}

// Logs returns everything the binary has written so far. Cetacean logs
// structured JSON to stderr, so this doubles as an assertion target.
func (p *Process) Logs() string { return p.output.String() }

// Stop reaps the child through the one shared reaper goroutine started in
// launch; it never calls Wait itself, only waits for that goroutine's result.
func (p *Process) Stop() {
	p.stopOnce.Do(func() {
		if p.cmd.Process == nil {
			return
		}

		_ = p.cmd.Process.Signal(os.Interrupt)

		select {
		case <-p.exited:
		case <-time.After(10 * time.Second):
			p.killed = true

			_ = p.cmd.Process.Kill()
			<-p.exited
		}
	})
}

// assertExitedCleanly fails the test if the binary did not exit cleanly on
// SIGINT, which is what an orchestrator sends on a rolling update. It is also
// where a race surfaces: a -race binary exits 66 once the detector has fired.
func (p *Process) assertExitedCleanly(t *testing.T) {
	t.Helper()

	if p.cmd.Process == nil {
		return
	}

	if p.killed {
		t.Errorf(
			"SUT did not exit within 10s of SIGINT and had to be killed; logs:\n%s",
			tailLines(p.Logs(), 40),
		)

		return
	}

	if p.waitErr != nil {
		t.Errorf(
			"SUT exited uncleanly after SIGINT: %v; logs:\n%s",
			p.waitErr, tailLines(p.Logs(), 40),
		)
	}
}

// tailLines keeps a failure message to the end of the log.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}

func (p *Process) waitReady() error {
	deadline := time.Now().Add(readyTimeout)

	for time.Now().Before(deadline) {
		resp, err := p.get(p.BaseURL + p.readyPath)
		if err == nil {
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-p.exited:
			return fmt.Errorf(
				"binary exited during startup with code %d",
				p.cmd.ProcessState.ExitCode(),
			)
		case <-time.After(pollInterval):
		}
	}

	return fmt.Errorf("binary not ready at %s within %s", p.BaseURL+p.readyPath, readyTimeout)
}

// waitPortFree blocks until the address the child is about to bind is free. It
// probes by binding, not dialing: http.Server stops accepting before its
// listener fd closes, so a refused dial reads as free. It binds the same
// wildcard ":<port>" the child gets, since a narrower probe can lie.
func waitPortFree(t *testing.T, port int) {
	t.Helper()

	addr := fmt.Sprintf(":%d", port)
	deadline := time.Now().Add(15 * time.Second)

	var lc net.ListenConfig

	for time.Now().Before(deadline) {
		ln, err := lc.Listen(context.Background(), "tcp", addr)
		if err == nil {
			ln.Close()

			return
		}

		time.Sleep(pollInterval)
	}

	t.Fatalf("port %d still in use after 15s", port)
}

// addrInUseOutput reports whether the child's captured output shows it failed
// to bind its listen address. Go formats EADDRINUSE as "...: bind: address
// already in use" on every platform this suite targets, and it is the only
// startup failure Start/StartExpectingExit retry rather than fail on.
func addrInUseOutput(output string) bool {
	return strings.Contains(output, "address already in use")
}

// binaryPath resolves the binary the harness supervises. CETACEAN_E2E_BINARY
// overrides it so `make test-stack` can run a coverage-instrumented build
// without replacing ./cetacean.
func binaryPath(t *testing.T) string {
	t.Helper()

	bin := os.Getenv("CETACEAN_E2E_BINARY")
	if bin == "" {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			t.Fatal("cannot locate sut source")
		}

		bin = filepath.Join(filepath.Dir(file), "..", "..", "..", "cetacean")
	}

	bin = filepath.Clean(bin)

	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("binary %s not found — run `make build` first: %v", bin, err)
	}

	return bin
}

// lockedBuffer is an io.Writer safe for the two pipes writing into it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}
