//go:build e2e

package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// PrometheusURL is where compose.e2e.yaml publishes the metrics lane's
// Prometheus. The SUT is a host process, so it reaches it over the published
// port rather than over the compose network.
const PrometheusURL = "http://127.0.0.1:19090"

// SeedInterval is the spacing between generated samples. It divides five
// minutes exactly, so a counter seeded with a constant step has an exact
// rate() over the [5m] window every query in the product uses.
const SeedInterval = 30 * time.Second

// SeedWindow is how far back the generated history reaches. Three hours is
// enough for every range preset the dashboard and get_metrics offer below a
// day, and for the sizing checker's quantile_over_time, which takes whatever
// falls inside its 168h subquery rather than requiring the window to be full.
const SeedWindow = 3 * time.Hour

// Series is one generated time series. A counter is expressed as a non-zero
// Step: the value rises by exactly that much every SeedInterval, so a query
// asserting `rate(...) == Step/SeedInterval` is asserting an exact number and
// not a tolerance. A gauge leaves Step at zero and holds Start.
type Series struct {
	Name   string
	Labels map[string]string
	Start  float64
	Step   float64
}

// PerSecond is the rate a counter series yields under rate(), which is what
// the assertions in the metrics lane are written against.
func (s Series) PerSecond() float64 {
	return s.Step / SeedInterval.Seconds()
}

// SeedPrometheus writes series into the lane's Prometheus as TSDB blocks and
// restarts it onto them.
//
// Blocks rather than scraping: real exporters would have to run for hours to
// produce a history worth querying, and would produce a different one every
// run. promtool builds the same blocks from the same input every time, and a
// week of history costs no wall clock — which is what makes an assertion on an
// exact rate() possible at all.
//
// The last sample is written at the moment of the call, so an instant query
// answers from it immediately; a lane that sits for more than five minutes
// before querying would fall outside Prometheus's lookback and see nothing.
func SeedPrometheus(t *testing.T, series []Series) time.Time {
	t.Helper()

	end := time.Now().Truncate(time.Second)
	file := filepath.Join(t.TempDir(), "seed.om")

	if err := os.WriteFile(file, []byte(openMetrics(series, end)), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	compose(t, "cp", file, "prometheus:/tmp/seed.om")

	// promtool refuses to write a block that would overlap one already in the
	// directory, so a second seed in the same run has to start from an empty
	// one. The lane seeds once; this is what keeps a second call from failing
	// in a way that reads as a Prometheus problem.
	compose(
		t,
		"exec",
		"-T",
		"prometheus",
		"sh",
		"-c",
		"rm -rf /prometheus/* && promtool tsdb create-blocks-from openmetrics /tmp/seed.om /prometheus",
	)

	// Blocks on disk are picked up when the database is opened, not while it
	// is running, so the restart is the load.
	compose(t, "restart", "prometheus")
	WaitForPrometheus(t)

	return end
}

// openMetrics renders the series as an OpenMetrics exposition ending at `end`.
// Every family is declared a gauge: the type is metadata, PromQL reads the
// samples either way, and declaring a counter would bind the exposition to
// OpenMetrics' `_total` naming rule for no gain here.
func openMetrics(series []Series, end time.Time) string {
	var out strings.Builder

	start := end.Add(-SeedWindow)
	steps := int(SeedWindow/SeedInterval) + 1

	for _, name := range slices.Sorted(maps.Keys(byName(series))) {
		fmt.Fprintf(&out, "# TYPE %s gauge\n", name)

		for _, s := range byName(series)[name] {
			for i := range steps {
				at := start.Add(time.Duration(i) * SeedInterval)
				value := s.Start + s.Step*float64(i)

				fmt.Fprintf(
					&out,
					"%s{%s} %g %d.000\n",
					s.Name,
					labelString(s.Labels),
					value,
					at.Unix(),
				)
			}
		}
	}

	out.WriteString("# EOF\n")

	return out.String()
}

func byName(series []Series) map[string][]Series {
	out := map[string][]Series{}
	for _, s := range series {
		out[s.Name] = append(out[s.Name], s)
	}

	return out
}

func labelString(labels map[string]string) string {
	pairs := make([]string, 0, len(labels))
	for _, key := range slices.Sorted(maps.Keys(labels)) {
		pairs = append(pairs, fmt.Sprintf("%s=%q", key, labels[key]))
	}

	sort.Strings(pairs)

	return strings.Join(pairs, ",")
}

// WaitForPrometheus blocks until the lane's Prometheus answers a query.
func WaitForPrometheus(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(upTimeout)
	var last error

	for time.Now().Before(deadline) {
		if err := queryOK(t.Context()); err == nil {
			return
		} else { //nolint:revive // the error is kept for the failure message
			last = err
		}

		time.Sleep(pollInterval)
	}

	t.Fatalf("prometheus did not become ready at %s: %v", PrometheusURL, last)
}

func queryOK(ctx context.Context) error {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		PrometheusURL+"/api/v1/query?query=vector(1)",
		nil,
	)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}

	if body.Status != "success" {
		return fmt.Errorf("status %q", body.Status)
	}

	return nil
}

func compose(t *testing.T, args ...string) {
	t.Helper()

	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}

	full := append(
		[]string{"compose", "-f", filepath.Join(root, "test", "e2e", "compose.e2e.yaml")},
		args...,
	)

	cmd := exec.CommandContext(t.Context(), "docker", full...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(full, " "), err, out)
	}
}
