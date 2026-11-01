package exec

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/errdefs"
)

// fakeDocker plays the daemon: the shell's side of the attach is server, and
// running reports whether the main exec is still alive.
type fakeDocker struct {
	mu        sync.Mutex
	noShell   bool
	server    net.Conn
	client    net.Conn
	created   []container.ExecOptions
	running   bool
	exitCode  int
	ignoreHUP bool
	ignoreAll bool
	resized   container.ResizeOptions
}

func newFakeDocker() *fakeDocker {
	client, server := net.Pipe()
	return &fakeDocker{client: client, server: server, running: true}
}

func (f *fakeDocker) ContainerStatPath(
	_ context.Context,
	_, path string,
) (container.PathStat, error) {
	if f.noShell {
		return container.PathStat{}, errdefs.NotFound(errors.New(path))
	}
	return container.PathStat{Name: "sh"}, nil
}

func (f *fakeDocker) ContainerExecCreate(
	_ context.Context,
	_ string,
	o container.ExecOptions,
) (container.ExecCreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, o)
	return container.ExecCreateResponse{ID: "exec-" + string(rune('0'+len(f.created)))}, nil
}

func (f *fakeDocker) ContainerExecAttach(
	context.Context,
	string,
	container.ExecAttachOptions,
) (types.HijackedResponse, error) {
	go func() { _, _ = f.server.Write([]byte("4242\r\n")) }()
	return types.HijackedResponse{Conn: f.client, Reader: bufio.NewReader(f.client)}, nil
}

func (f *fakeDocker) ContainerExecStart(
	_ context.Context,
	id string,
	_ container.ExecStartOptions,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := strings.Join(f.created[len(f.created)-1].Cmd, " ")
	switch {
	case strings.Contains(cmd, "kill -HUP 4242") && !f.ignoreHUP && !f.ignoreAll:
		f.running, f.exitCode = false, 129
	case strings.Contains(cmd, "kill -KILL 4242") && !f.ignoreAll:
		f.running, f.exitCode = false, 137
	}
	return nil
}

func (f *fakeDocker) ContainerExecResize(
	_ context.Context,
	_ string,
	o container.ResizeOptions,
) error {
	f.resized = o
	return nil
}

func (f *fakeDocker) ContainerExecInspect(context.Context, string) (container.ExecInspect, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return container.ExecInspect{Running: f.running, ExitCode: f.exitCode}, nil
}

// openLocal keeps the settle window and grace short so Close-based tests stay
// fast; both are fields on Local now, not package-level state.
func openLocal(t *testing.T, d *fakeDocker) Stream {
	t.Helper()
	l := &Local{
		Docker:           d,
		NodeID:           func() string { return "n1" },
		Grace:            50 * time.Millisecond,
		SettlePollWindow: 50 * time.Millisecond,
		SettlePollStep:   5 * time.Millisecond,
	}
	s, err := l.Open(
		context.Background(),
		Request{ContainerID: "c1", NodeID: "n1", Cols: 80, Rows: 24},
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func TestLocalReachesOnlyItsOwnNode(t *testing.T) {
	l := &Local{NodeID: func() string { return "n1" }}
	if !l.Reaches("n1") || l.Reaches("n2") || l.Reaches("") {
		t.Error("Reaches is wrong")
	}
	if (&Local{NodeID: func() string { return "" }}).Reaches("") {
		t.Error("an unknown local node reached an empty node ID")
	}
}

func TestLocalOpenReportsNoShell(t *testing.T) {
	d := newFakeDocker()
	d.noShell = true
	l := &Local{Docker: d, NodeID: func() string { return "n1" }}
	if _, err := l.Open(
		context.Background(),
		Request{ContainerID: "c1"},
	); !errors.Is(
		err,
		ErrNoShell,
	) {
		t.Fatalf("err = %v, want ErrNoShell", err)
	}
}

func TestLocalOpenPinsExecOptionsAndConsumesPID(t *testing.T) {
	d := newFakeDocker()
	s := openLocal(t, d)

	o := d.created[0]
	if o.Privileged || o.User != "" || o.WorkingDir != "" || len(o.Env) != 0 || !o.Tty {
		t.Errorf("exec options not pinned: %+v", o)
	}
	if got := strings.Join(o.Cmd, " "); !strings.HasPrefix(got, `/bin/sh -c echo "$$";`) {
		t.Errorf("cmd = %q", got)
	}

	go func() { _, _ = d.server.Write([]byte("$ ")) }()
	buf := make([]byte, 16)
	n, _ := s.Read(buf)
	if string(buf[:n]) != "$ " {
		t.Errorf("first read = %q: the PID line leaked into the terminal", buf[:n])
	}
}

func TestLocalCloseAfterShellExitedReportsExited(t *testing.T) {
	d := newFakeDocker()
	s := openLocal(t, d)
	d.mu.Lock()
	d.running, d.exitCode = false, 3
	d.mu.Unlock()

	if got := s.Close(context.Background()); got != (Exit{Code: 3, Outcome: OutcomeExited}) {
		t.Errorf("exit = %+v", got)
	}
}

func TestLocalCloseWithoutEOFSignalsWithoutWaitingTheSettleWindow(t *testing.T) {
	d := newFakeDocker()
	s := openLocal(t, d)
	// No Read happened, so output EOF was never observed: Close must check
	// once and signal immediately rather than polling the settle window.

	start := time.Now()
	got := s.Close(context.Background())
	elapsed := time.Since(start)

	if got != (Exit{Code: 129, Outcome: OutcomeTerminated}) {
		t.Errorf("exit = %+v", got)
	}
	if elapsed >= 50*time.Millisecond {
		t.Errorf("close took %s before signalling; the settle window should not apply", elapsed)
	}
}

func TestLocalCloseAfterEOFPollsBeforeSignalling(t *testing.T) {
	d := newFakeDocker()
	s := openLocal(t, d)

	d.mu.Lock()
	d.running, d.exitCode = true, 0
	d.mu.Unlock()

	d.server.Close()
	if _, err := s.Read(make([]byte, 16)); err == nil {
		t.Fatal("expected an error reading after closing the fake's server side")
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		d.mu.Lock()
		d.running, d.exitCode = false, 7
		d.mu.Unlock()
	}()

	if got := s.Close(context.Background()); got != (Exit{Code: 7, Outcome: OutcomeExited}) {
		t.Errorf("exit = %+v", got)
	}
	for _, o := range d.created {
		if strings.Contains(strings.Join(o.Cmd, " "), "kill -") {
			t.Errorf("a signal was sent although the shell exited on its own: %+v", d.created)
		}
	}
}

func TestLocalCloseTerminatesWithHUP(t *testing.T) {
	d := newFakeDocker()
	s := openLocal(t, d)

	got := s.Close(context.Background())
	if got != (Exit{Code: 129, Outcome: OutcomeTerminated}) {
		t.Errorf("exit = %+v", got)
	}
	if last := strings.Join(
		d.created[len(d.created)-1].Cmd,
		" ",
	); !strings.Contains(
		last,
		"kill -HUP 4242",
	) {
		t.Errorf("last exec = %q, want a HUP to the shell's in-container PID", last)
	}
}

func TestLocalCloseEscalatesToKILL(t *testing.T) {
	d := newFakeDocker()
	d.ignoreHUP = true
	s := openLocal(t, d)

	if got := s.Close(context.Background()); got != (Exit{Code: 137, Outcome: OutcomeTerminated}) {
		t.Errorf("exit = %+v", got)
	}
}

func TestLocalCloseReportsDetached(t *testing.T) {
	d := newFakeDocker()
	d.ignoreAll = true
	s := openLocal(t, d)

	if got := s.Close(context.Background()); got != (Exit{Code: -1, Outcome: OutcomeDetached}) {
		t.Errorf("exit = %+v", got)
	}
}

func TestLocalCloseIsIdempotent(t *testing.T) {
	d := newFakeDocker()
	s := openLocal(t, d)

	first := s.Close(context.Background())
	second := s.Close(context.Background())

	if first != (Exit{Code: 129, Outcome: OutcomeTerminated}) {
		t.Fatalf("first close = %+v", first)
	}
	if second != first {
		t.Errorf("second close = %+v, want the cached first result %+v", second, first)
	}
}

func TestLocalCloseIsSafeForConcurrentCallers(t *testing.T) {
	d := newFakeDocker()
	s := openLocal(t, d)

	const callers = 10
	results := make([]Exit, callers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = s.Close(context.Background())
		}(i)
	}
	wg.Wait()

	want := Exit{Code: 129, Outcome: OutcomeTerminated}
	for i, got := range results {
		if got != want {
			t.Errorf("result[%d] = %+v, want %+v", i, got, want)
		}
	}
}

func TestLocalCloseStillSignalsWithACancelledContext(t *testing.T) {
	d := newFakeDocker()
	s := openLocal(t, d)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := s.Close(ctx)
	if got != (Exit{Code: 129, Outcome: OutcomeTerminated}) {
		t.Errorf("exit = %+v", got)
	}
	if last := strings.Join(
		d.created[len(d.created)-1].Cmd,
		" ",
	); !strings.Contains(
		last,
		"kill -HUP 4242",
	) {
		t.Errorf("last exec = %q, want a HUP despite the cancelled context", last)
	}
}

func TestLocalResizePassesRowsAsHeight(t *testing.T) {
	d := newFakeDocker()
	s := openLocal(t, d)
	if err := s.Resize(context.Background(), 120, 40); err != nil {
		t.Fatal(err)
	}
	if d.resized.Width != 120 || d.resized.Height != 40 {
		t.Errorf("resize = %+v", d.resized)
	}
}
