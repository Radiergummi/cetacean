package compose

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

// duration renders a nanosecond count as compose's duration string. The zero
// value renders empty so the field is omitted rather than written as "0s".
func duration(d time.Duration) string {
	if d == 0 {
		return ""
	}

	return d.String()
}

// cpus renders NanoCPUs as compose's fractional core count, exactly: rounding
// to two places turns a 0.125 reservation into a 0.13 the service never had,
// which is the claim memory refuses to make just below.
func cpus(nano int64) string {
	if nano == 0 {
		return ""
	}

	whole, frac := nano/nanosPerCPU, nano%nanosPerCPU
	if frac == 0 {
		return strconv.FormatInt(whole, 10)
	}

	return strings.TrimRight(fmt.Sprintf("%d.%09d", whole, frac), "0")
}

const nanosPerCPU = 1e9

// memory renders a byte count with a binary suffix, but only when the suffix
// divides evenly: a rounded figure would claim a limit the service does not
// have.
func memory(bytes int64) string {
	if bytes == 0 {
		return ""
	}

	for _, unit := range []struct {
		size   int64
		suffix string
	}{
		{1 << 30, "G"},
		{1 << 20, "M"},
		{1 << 10, "K"},
	} {
		if bytes%unit.size == 0 {
			return strconv.FormatInt(bytes/unit.size, 10) + unit.suffix
		}
	}

	return strconv.FormatInt(bytes, 10)
}

// environment turns Swarm's K=V slice into compose's map. A bare K carries no
// value and becomes a null, which is what tells the daemon to pass its own
// through; an empty string would instead set the variable to nothing.
func environment(env []string) map[string]*string {
	if len(env) == 0 {
		return nil
	}

	out := make(map[string]*string, len(env))
	for _, e := range env {
		name, value, ok := strings.Cut(e, "=")
		if !ok {
			out[name] = nil
			continue
		}

		out[name] = &value
	}

	return out
}

// extraHosts reverses hosts(5) order into compose's "host:ip", one entry per
// alias.
func extraHosts(hosts []string) []string {
	var out []string
	for _, h := range hosts {
		fields := strings.Fields(h)
		if len(fields) < 2 {
			continue
		}

		for _, name := range fields[1:] {
			out = append(out, name+":"+fields[0])
		}
	}

	return out
}

// mounts renders compose's long syntax. A named volume loses the stack prefix;
// a bind source is a host path and is passed through, since shortening it
// would rewrite the filesystem. Options compose has no field for are reported
// rather than dropped: they change what the mount does.
func mounts(ms []mount.Mount, n names) ([]ServiceVolume, []string) {
	out := make([]ServiceVolume, 0, len(ms))

	var warnings []string

	for _, m := range ms {
		v := ServiceVolume{
			Type:     string(m.Type),
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		}
		if m.Type == mount.TypeVolume {
			v.Source = n.short(kindVolume, m.Source)
		}

		if o := m.VolumeOptions; o != nil {
			if o.NoCopy || o.Subpath != "" {
				v.Volume = &VolumeOpts{NoCopy: o.NoCopy, Subpath: o.Subpath}
			}
			// Both belong to the volume rather than to this mount of it, and
			// compose only accepts them where the volume is declared.
			if o.DriverConfig != nil {
				warnings = append(warnings, dropped(m.Target, "volume driver options"))
			}
			if len(o.Labels) > 0 {
				warnings = append(warnings, dropped(m.Target, "volume labels"))
			}
		}

		if o := m.BindOptions; o != nil {
			if o.Propagation != "" {
				v.Bind = &BindOpts{Propagation: string(o.Propagation)}
			}
			if o.NonRecursive || o.ReadOnlyNonRecursive || o.ReadOnlyForceRecursive {
				warnings = append(warnings, dropped(m.Target, "bind recursion flags"))
			}
		}

		if o := m.TmpfsOptions; o != nil {
			if o.SizeBytes != 0 || o.Mode != 0 {
				v.Tmpfs = &TmpfsOpts{Size: o.SizeBytes, Mode: uint32(o.Mode)}
			}
			if len(o.Options) > 0 {
				warnings = append(warnings, dropped(m.Target, "tmpfs mount options"))
			}
		}

		out = append(out, v)
	}

	return out, warnings
}

// dropped names what a mount carried and the document cannot.
func dropped(target, what string) string {
	return "mount " + target + ": " + what + " dropped: compose has no field for them"
}

// ulimits renders compose's long form, which is the only one that carries a
// soft and a hard limit at once.
func ulimits(us []*container.Ulimit) map[string]Ulimit {
	if len(us) == 0 {
		return nil
	}

	out := make(map[string]Ulimit, len(us))
	for _, u := range us {
		if u != nil {
			out[u.Name] = Ulimit{Soft: u.Soft, Hard: u.Hard}
		}
	}

	return out
}

func ports(p []swarm.PortConfig) []ServicePort {
	out := make([]ServicePort, 0, len(p))
	for _, c := range p {
		out = append(out, ServicePort{
			Target:    c.TargetPort,
			Published: c.PublishedPort,
			Protocol:  string(c.Protocol),
			Mode:      string(c.PublishMode),
		})
	}

	return out
}

func healthcheck(h *container.HealthConfig) *Healthcheck {
	if h == nil {
		return nil
	}

	return &Healthcheck{
		Test:          h.Test,
		Interval:      duration(h.Interval),
		Timeout:       duration(h.Timeout),
		Retries:       h.Retries,
		StartPeriod:   duration(h.StartPeriod),
		StartInterval: duration(h.StartInterval),
	}
}

// privileges splits Swarm's one field across compose's three. A custom seccomp
// profile has no compose representation at all, so it is reported rather than
// silently dropped.
func privileges(p *swarm.Privileges) (map[string]string, []string, string) {
	if p == nil {
		return nil, nil, ""
	}

	var (
		credSpec map[string]string
		opts     []string
		warning  string
	)

	if c := p.CredentialSpec; c != nil {
		credSpec = map[string]string{}
		switch {
		case c.File != "":
			credSpec["file"] = c.File
		case c.Registry != "":
			credSpec["registry"] = c.Registry
		case c.Config != "":
			credSpec["config"] = c.Config
		}
	}

	if s := p.SELinuxContext; s != nil {
		for _, kv := range []struct{ key, value string }{
			{"user", s.User}, {"role", s.Role}, {"type", s.Type}, {"level", s.Level},
		} {
			if kv.value != "" {
				opts = append(opts, "label="+kv.key+":"+kv.value)
			}
		}
	}

	if a := p.AppArmor; a != nil && a.Mode != "" {
		opts = append(opts, "apparmor="+string(a.Mode))
	}

	if p.NoNewPrivileges {
		opts = append(opts, "no-new-privileges:true")
	}

	if s := p.Seccomp; s != nil && len(s.Profile) > 0 {
		warning = "custom seccomp profile dropped: compose has no field for it"
	}

	return credSpec, opts, warning
}
