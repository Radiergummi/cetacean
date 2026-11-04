//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file opens shells through the real binary against containers on the
// single DinD node, where the Docker behaviour the exec design leaves
// unverified lives: the PID line through a TTY, and HUP ending the shell's
// foreground child. It reserves port 19030.

const execPort = 19030

const execPolicy = `grants:
  - resources: ["*"]
    audience: ["group:ops"]
    permissions: ["read", "exec"]
`

func startExecLane(t *testing.T, env *harness.Env) *sut.Process {
	t.Helper()

	policy := filepath.Join(t.TempDir(), "acl.yaml")
	if err := os.WriteFile(policy, []byte(execPolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	return sut.Start(t, sut.Config{
		Port:       execPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":            "headers",
			"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
			"CETACEAN_AUTH_HEADERS_GROUPS":  "X-Auth-Groups",
			"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
			"CETACEAN_ACL_POLICY_FILE":      policy,
			"CETACEAN_OPERATIONS_LEVEL":     "3",
			"CETACEAN_EXEC":                 "true",
		},
	})
}

func execHeaders() http.Header {
	return http.Header{
		"X-Auth-User":   {opsPersona.user},
		"X-Auth-Groups": {opsPersona.groups},
	}
}

// deployShellTarget deploys an idle service and waits until Cetacean's cache
// holds its task running, returning the task and its container.
func deployShellTarget(
	t *testing.T,
	env *harness.Env,
	proc *sut.Process,
	stack string,
) (taskID, containerID string) {
	t.Helper()

	service := fixtures.DeployStack(t, env, stack, []fixtures.ServiceSpec{{
		Name: "app", Replicas: 1, Command: []string{"sleep infinity"},
	}}) + "_app"

	var body struct {
		Items []struct {
			ID          string `json:"ID"`
			ServiceName string `json:"ServiceName"`
			Status      struct {
				State           string `json:"State"`
				ContainerStatus *struct {
					ContainerID string `json:"ContainerID"`
				} `json:"ContainerStatus"`
			} `json:"Status"`
		} `json:"items"`
	}

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp := readAs(t, proc, opsPersona, "/tasks?limit=200", "")
		err := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("decode /tasks: %v", err)
		}

		for _, item := range body.Items {
			if item.ServiceName == service && item.Status.State == "running" &&
				item.Status.ContainerStatus != nil && item.Status.ContainerStatus.ContainerID != "" {
				return item.ID, item.Status.ContainerStatus.ContainerID
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("Cetacean never served a running task of %s", service)

	return "", ""
}

// terminal is an attached session, keeping the shell's output as it arrives.
type terminal struct {
	conn    *websocket.Conn
	session string
	output  strings.Builder
}

func openTerminal(t *testing.T, proc *sut.Process, taskID string) *terminal {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, proc.BaseURL+"/tasks/"+taskID+"/exec", nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header = execHeaders()
	req.Header.Set("Accept", "application/json")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /tasks/%s/exec: %v", taskID, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /tasks/%s/exec: status = %d, want 201", taskID, resp.StatusCode)
	}
	location := resp.Header.Get("Location")

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	//nolint:bodyclose // coder/websocket owns the upgrade response's body
	conn, _, err := websocket.Dial(ctx,
		"ws"+strings.TrimPrefix(proc.BaseURL, "http")+location,
		&websocket.DialOptions{HTTPHeader: execHeaders()})
	if err != nil {
		t.Fatalf("dial %s: %v", location, err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	return &terminal{conn: conn, session: location}
}

// await reads frames until the shell's output contains want, or a text frame
// does, and returns that frame or the output.
func (term *terminal) await(t *testing.T, want string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	for {
		typ, data, err := term.conn.Read(ctx)
		if err != nil {
			t.Fatalf("read while waiting for %q: %v (output so far %q)",
				want, err, term.output.String())
		}

		if typ == websocket.MessageText {
			if strings.Contains(string(data), want) {
				return string(data)
			}

			continue
		}

		term.output.Write(data)
		if strings.Contains(term.output.String(), want) {
			return term.output.String()
		}
	}
}

func (term *terminal) send(t *testing.T, input string) {
	t.Helper()

	if err := term.conn.Write(t.Context(), websocket.MessageBinary, []byte(input)); err != nil {
		t.Fatalf("write %q: %v", input, err)
	}
}

type execSession struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
	Exit   *struct {
		Code    int    `json:"code"`
		Outcome string `json:"outcome"`
	} `json:"exit"`
}

func awaitSessionEnded(t *testing.T, proc *sut.Process, path string) execSession {
	t.Helper()

	var s execSession
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp := readAs(t, proc, opsPersona, path, "")
		err := json.NewDecoder(resp.Body).Decode(&s)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if s.State == "ended" {
			return s
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Fatalf("session %s never ended: %+v", path, s)

	return s
}

var pidLine = regexp.MustCompile(`^\d+\r?\n`)

func TestExecOpensAShellWithoutLeakingThePID(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	proc := startExecLane(t, env)
	taskID, _ := deployShellTarget(t, env, proc, "execsh")

	term := openTerminal(t, proc, taskID)
	term.await(t, `"connected"`)

	term.send(t, "echo ce2e-$((40+2))\r")
	out := term.await(t, "ce2e-42")
	if pidLine.MatchString(out) {
		t.Errorf("the output opens with the shell's PID line: %q", out)
	}

	term.send(t, "exit 7\r")
	exit := term.await(t, `"type":"exit"`)

	var frame struct {
		Code    *int   `json:"code"`
		Reason  string `json:"reason"`
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(exit), &frame); err != nil {
		t.Fatalf("decode exit frame %q: %v", exit, err)
	}
	if frame.Code == nil || *frame.Code != 7 || frame.Outcome != "exited" || frame.Reason != "" {
		t.Errorf("exit frame = %s, want code 7, outcome exited, no reason", exit)
	}

	if s := awaitSessionEnded(t, proc, term.session); s.Reason != "exited" {
		t.Errorf("session reason = %q, want exited", s.Reason)
	}
}

func TestExecDisconnectTerminatesTheForegroundProcess(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	proc := startExecLane(t, env)
	taskID, containerID := deployShellTarget(t, env, proc, "exechup")

	term := openTerminal(t, proc, taskID)
	term.await(t, `"connected"`)
	term.send(t, "sleep 9999\r")

	running := func() bool {
		return strings.Contains(env.Exec(t, containerID, "ps", "-o", "args"), "sleep 9999")
	}
	deadline := time.Now().Add(10 * time.Second)
	for !running() {
		if time.Now().After(deadline) {
			t.Fatal("sleep 9999 never started in the shell")
		}
		time.Sleep(250 * time.Millisecond)
	}

	_ = term.conn.CloseNow()

	s := awaitSessionEnded(t, proc, term.session)
	if s.Reason != "disconnected" || s.Exit == nil || s.Exit.Outcome != "terminated" {
		t.Errorf("session = %+v exit %+v, want reason disconnected, outcome terminated", s, s.Exit)
	}

	// An orphaned foreground process is the failure the exec design warns about.
	deadline = time.Now().Add(10 * time.Second)
	for running() {
		if time.Now().After(deadline) {
			t.Fatalf("sleep 9999 outlived the session:\n%s",
				env.Exec(t, containerID, "ps", "-o", "pid,args"))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// An attached session is listed cluster-wide and under its task, and ending it
// over HTTP closes the shell with the caller named as who ended it.
func TestExecSessionsAreListedAndEnded(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	proc := startExecLane(t, env)
	taskID, _ := deployShellTarget(t, env, proc, "execend")

	term := openTerminal(t, proc, taskID)
	term.await(t, `"connected"`)

	for _, path := range []string{"/exec", "/tasks/" + taskID + "/exec"} {
		var body struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}

		resp := readAs(t, proc, opsPersona, path, "")
		err := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d, decode %v", path, resp.StatusCode, err)
		}

		listed := false
		for _, item := range body.Items {
			listed = listed || "/exec/"+item.ID == term.session
		}
		if !listed {
			t.Errorf("GET %s does not list %s: %+v", path, term.session, body.Items)
		}
	}

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodDelete, proc.BaseURL+term.session, nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header = execHeaders()

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", term.session, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE %s: status = %d, want 204", term.session, resp.StatusCode)
	}

	if s := awaitSessionEnded(t, proc, term.session); s.Reason != "terminated_by" {
		t.Errorf("session reason = %q, want terminated_by", s.Reason)
	}
}
