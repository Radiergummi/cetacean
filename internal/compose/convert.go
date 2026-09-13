package compose

import (
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

// cpus renders NanoCPUs as compose's fractional core count.
func cpus(nano int64) string {
	if nano == 0 {
		return ""
	}

	return strconv.FormatFloat(float64(nano)/1e9, 'f', 2, 64)
}

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

// mounts renders compose's long syntax. shorten strips a stack prefix from a
// named volume; a bind source is a host path and is passed through, since
// shortening it would rewrite the filesystem.
func mounts(ms []mount.Mount, shorten func(string) string) []ServiceVolume {
	out := make([]ServiceVolume, 0, len(ms))
	for _, m := range ms {
		v := ServiceVolume{
			Type:     string(m.Type),
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		}
		if m.Type == mount.TypeVolume {
			v.Source = shorten(m.Source)
		}

		out = append(out, v)
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
