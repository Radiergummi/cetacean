package compose

import (
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

func TestDuration(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{10 * time.Second, "10s"},
		{90 * time.Second, "1m30s"},
		{0, ""},
		{500 * time.Millisecond, "500ms"},
	} {
		if got := duration(tc.in); got != tc.want {
			t.Errorf("duration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCPUs(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{500000000, "0.50"},
		{1000000000, "1.00"},
		{2500000000, "2.50"},
		{0, ""},
	} {
		if got := cpus(tc.in); got != tc.want {
			t.Errorf("cpus(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Byte suffixes only when they divide evenly — 536870912 is 512M and says so,
// 536870913 is not and must not be rounded into a lie.
func TestMemory(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{536870912, "512M"},
		{1073741824, "1G"},
		{1024, "1K"},
		{536870913, "536870913"},
		{0, ""},
	} {
		if got := memory(tc.in); got != tc.want {
			t.Errorf("memory(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A bare K with no = means "inherit from the daemon" and maps to a null
// value, which is not the same as an empty string.
func TestEnvironment(t *testing.T) {
	got := environment([]string{"A=1", "B=", "C", "D=x=y"})

	if got["A"] == nil || *got["A"] != "1" {
		t.Errorf("A = %v, want \"1\"", got["A"])
	}
	if got["B"] == nil || *got["B"] != "" {
		t.Errorf("B = %v, want empty string", got["B"])
	}
	if v, ok := got["C"]; !ok || v != nil {
		t.Errorf("C = %v, want nil", v)
	}
	if got["D"] == nil || *got["D"] != "x=y" {
		t.Errorf("D = %v, want \"x=y\"", got["D"])
	}
	if environment(nil) != nil {
		t.Error("no env must produce no map, not an empty one")
	}
}

// Swarm writes hosts(5) order, "IP canonical [aliases...]"; compose writes
// "host:IP". The fields reverse and each alias becomes its own entry.
func TestExtraHosts(t *testing.T) {
	got := extraHosts([]string{"1.2.3.4 db db.internal", "5.6.7.8 cache"})
	want := []string{"db:1.2.3.4", "db.internal:1.2.3.4", "cache:5.6.7.8"}

	if len(got) != len(want) {
		t.Fatalf("extraHosts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("extraHosts[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if extraHosts([]string{"   "}) != nil {
		t.Error("a blank entry must be dropped, not emitted as \":\"")
	}
}

func TestMountsUseLongSyntaxAndShortenTheSource(t *testing.T) {
	got := mounts([]mount.Mount{
		{Type: mount.TypeVolume, Source: "web_data", Target: "/data"},
		{Type: mount.TypeBind, Source: "/etc/hosts", Target: "/etc/hosts", ReadOnly: true},
	}, forStack("web", nil))

	if got[0].Type != "volume" || got[0].Source != "data" || got[0].Target != "/data" {
		t.Errorf("volume mount = %+v", got[0])
	}
	if got[0].ReadOnly {
		t.Error("mount must not claim read-only when it is not")
	}
	// A bind source is a host path, not a stack resource: shortening it would
	// rewrite the filesystem path.
	if got[1].Source != "/etc/hosts" || !got[1].ReadOnly {
		t.Errorf("bind mount = %+v", got[1])
	}
}

func TestPortsMapPublishMode(t *testing.T) {
	got := ports([]swarm.PortConfig{
		{
			TargetPort:    80,
			PublishedPort: 8080,
			Protocol:      "tcp",
			PublishMode:   swarm.PortConfigPublishModeIngress,
		},
		{
			TargetPort:    53,
			PublishedPort: 5353,
			Protocol:      "udp",
			PublishMode:   swarm.PortConfigPublishModeHost,
		},
	})

	if got[0].Mode != "ingress" || got[0].Target != 80 || got[0].Published != 8080 {
		t.Errorf("ingress port = %+v", got[0])
	}
	if got[1].Mode != "host" || got[1].Protocol != "udp" {
		t.Errorf("host port = %+v", got[1])
	}
}

func TestHealthcheckDurationsBecomeStrings(t *testing.T) {
	got := healthcheck(&container.HealthConfig{
		Test:          []string{"CMD", "curl", "-f", "http://localhost/"},
		Interval:      30 * time.Second,
		Timeout:       5 * time.Second,
		StartPeriod:   time.Minute,
		StartInterval: 2 * time.Second,
		Retries:       3,
	})

	if got.Interval != "30s" || got.Timeout != "5s" || got.StartPeriod != "1m0s" ||
		got.StartInterval != "2s" {
		t.Errorf("healthcheck = %+v", got)
	}
	if got.Retries != 3 || len(got.Test) != 4 {
		t.Errorf("healthcheck = %+v", got)
	}
	if healthcheck(nil) != nil {
		t.Error("no healthcheck must produce nil")
	}
}

func TestPrivilegesSplitAndWarnOnSeccomp(t *testing.T) {
	spec, opts, warn := privileges(&swarm.Privileges{
		CredentialSpec: &swarm.CredentialSpec{File: "spec.json"},
		SELinuxContext: &swarm.SELinuxContext{Level: "s0:c1,c2"},
		Seccomp:        &swarm.SeccompOpts{Profile: []byte(`{"defaultAction":"SCMP_ACT_ERRNO"}`)},
	})

	if spec["file"] != "spec.json" {
		t.Errorf("credential_spec = %v", spec)
	}
	if len(opts) == 0 || !strings.Contains(opts[0], "s0:c1,c2") {
		t.Errorf("security_opt = %v", opts)
	}
	if warn == "" || !strings.Contains(warn, "seccomp") {
		t.Errorf("a custom seccomp profile must warn, got %q", warn)
	}

	_, opts, _ = privileges(&swarm.Privileges{NoNewPrivileges: true})
	if len(opts) != 1 || opts[0] != "no-new-privileges:true" {
		t.Errorf("security_opt = %v, want no-new-privileges:true", opts)
	}

	if _, _, w := privileges(nil); w != "" {
		t.Errorf("no privileges must not warn, got %q", w)
	}
}
