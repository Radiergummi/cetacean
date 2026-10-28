package cluster

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/radiergummi/cetacean/internal/docker"
)

func TestClassifyWrite(t *testing.T) {
	const internals = "/var/run/docker.sock on node-7"

	engine := func(class error) error { return fmt.Errorf("%w: %s", class, internals) }
	dial := &net.OpError{
		Op: "dial", Net: "unix",
		Addr: &net.UnixAddr{Name: internals, Net: "unix"},
		Err:  syscall.ECONNREFUSED,
	}

	cases := []struct {
		name  string
		err   error
		kind  WriteErrorKind
		shown bool
	}{
		{"not found", engine(cerrdefs.ErrNotFound), NotFound, false},
		{"invalid", engine(cerrdefs.ErrInvalidArgument), Invalid, true},
		{"unavailable", engine(cerrdefs.ErrUnavailable), Unavailable, true},
		{"conflict", engine(cerrdefs.ErrConflict), Conflict, true},
		{"failed precondition", engine(cerrdefs.ErrFailedPrecondition), Conflict, false},
		{
			"out of sequence",
			errors.New("rpc error: update out of sequence " + internals),
			Stale,
			false,
		},
		{
			"stale pin",
			&docker.StaleVersionError{Kind: "service", ID: internals, Pinned: 3, Current: 4},
			Stale,
			false,
		},
		{"internal", engine(cerrdefs.ErrInternal), Engine, false},
		{"unknown", engine(cerrdefs.ErrUnknown), Engine, false},
		{"permission", engine(cerrdefs.ErrPermissionDenied), Engine, false},
		{"unreachable", fmt.Errorf("error during connect: %w", dial), Engine, false},
		{"host access", &HostAccessError{What: "adding a bind mount"}, HostAccess, true},
		{"attach denied", &AttachDeniedError{Resource: "secret:db"}, Denied, true},
		{"stack label denied", &StackLabelDeniedError{Stack: "billing"}, Denied, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refusal, ok := ClassifyWrite(tc.err, "service", "web")
			if !ok {
				t.Fatalf("ClassifyWrite(%v) did not classify it", tc.err)
			}
			if refusal.Kind != tc.kind {
				t.Errorf("Kind = %d, want %d", refusal.Kind, tc.kind)
			}
			if !errors.Is(refusal, tc.err) {
				t.Errorf("Err = %v, want the cause", refusal.Err)
			}

			shown := refusal.Detail != "" && strings.Contains(tc.err.Error(), refusal.Detail)
			if strings.Contains(refusal.Detail, internals) && !tc.shown {
				t.Errorf("Detail %q carries the withheld text", refusal.Detail)
			}
			if tc.shown && !shown {
				t.Errorf("Detail %q, want the error's own text", refusal.Detail)
			}
			if tc.kind != Engine && refusal.Withheld() == tc.shown {
				t.Errorf("Withheld = %t, want %t", refusal.Withheld(), !tc.shown)
			}
		})
	}
}

func TestClassifyWriteLeavesCetaceansOwnErrors(t *testing.T) {
	if refusal, ok := ClassifyWrite(errors.New(`section "ports": bad`), "service", "web"); ok {
		t.Errorf("ClassifyWrite = %+v, want it left to the caller", refusal)
	}
}

func TestClassifyRemoval(t *testing.T) {
	err := fmt.Errorf(
		"%w: rpc error: code = FailedPrecondition desc = network abc is in use by service secret-db",
		cerrdefs.ErrInvalidArgument,
	)

	refusal, ok := ClassifyRemoval(err, "network", "abc")
	if !ok || refusal.Kind != Conflict {
		t.Fatalf("ClassifyRemoval = %+v, %t, want a conflict", refusal, ok)
	}
	if refusal.Detail != RemovalConflictDetail("network", "abc") {
		t.Errorf("Detail = %q, want the removal's own", refusal.Detail)
	}

	refusal, ok = ClassifyRemoval(
		fmt.Errorf("%w: boom", cerrdefs.ErrInvalidArgument),
		"network",
		"abc",
	)
	if !ok || refusal.Kind != Invalid {
		t.Errorf("an unmarked removal error classified as %+v, %t, want Invalid", refusal, ok)
	}
}
