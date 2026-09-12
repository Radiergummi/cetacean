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
// restarts it onto them. Blocks rather than scraping: promtool builds the same
// week of history every run, which is what makes an exact rate() assertion
// possible. The last sample lands at call time, inside Prometheus's lookback.
func SeedPrometheus(t *testing.T, series []Series) time.Time {
	t.Helper()

	end, err := SeedPrometheusCLI(t.Context(), series)
	if err != nil {
		t.Fatalf("seed prometheus: %v", err)
	}

	return end
}

// SeedPrometheusCLI is SeedPrometheus's non-test entry point, for callers with
// no *testing.T. `make e2e-up` seeds the same series the Go lane does, so the
// browser suite and metrics_test.go look at one cluster.
func SeedPrometheusCLI(ctx context.Context, series []Series) (time.Time, error) {
	end := time.Now().Truncate(time.Second)

	dir, err := os.MkdirTemp("", "cetacean-seed")
	if err != nil {
		return end, err
	}
	defer os.RemoveAll(dir)

	file := filepath.Join(dir, "seed.om")
	if err := os.WriteFile(file, []byte(openMetrics(series, end)), 0o644); err != nil {
		return end, fmt.Errorf("write seed: %w", err)
	}

	if err := compose(ctx, "cp", file, "prometheus:/tmp/seed.om"); err != nil {
		return end, err
	}

	// promtool refuses to write a block overlapping one already in the
	// directory, so a seed starts from an empty one. That is also what makes
	// seeding idempotent: `make e2e-up` twice replaces the history rather than
	// failing on it.
	if err := compose(
		ctx,
		"exec",
		"-T",
		"prometheus",
		"sh",
		"-c",
		"rm -rf /prometheus/* && promtool tsdb create-blocks-from openmetrics /tmp/seed.om /prometheus",
	); err != nil {
		return end, err
	}

	// Blocks on disk are picked up when the database is opened, not while it
	// is running, so the restart is the load.
	if err := compose(ctx, "restart", "prometheus"); err != nil {
		return end, err
	}

	return end, waitForPrometheus(ctx)
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

// waitForPrometheus blocks until the lane's Prometheus answers a query.
func waitForPrometheus(ctx context.Context) error {
	deadline := time.Now().Add(upTimeout)

	var last error

	for time.Now().Before(deadline) {
		if err := queryOK(ctx); err == nil {
			return nil
		} else { //nolint:revive // the error is kept for the failure message
			last = err
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("prometheus did not become ready at %s: %w", PrometheusURL, last)
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

func compose(ctx context.Context, args ...string) error {
	root, err := repoRoot()
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}

	full := append(
		[]string{"compose", "-f", filepath.Join(root, "test", "e2e", "compose.e2e.yaml")},
		args...,
	)

	out, err := exec.CommandContext(ctx, "docker", full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w\n%s", strings.Join(full, " "), err, out)
	}

	return nil
}
