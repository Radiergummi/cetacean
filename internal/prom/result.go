package prom

// Result holds a single Prometheus query result with metric labels and a scalar value.
type Result struct {
	Labels map[string]string
	Value  float64
}
