package compose

import (
	"testing"
	"time"
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
