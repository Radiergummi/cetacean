package compose

import (
	"strconv"
	"strings"
	"time"
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
