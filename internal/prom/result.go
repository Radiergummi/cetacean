package prom

// Result holds a single Prometheus query result with metric labels and a scalar value.
type Result struct {
	Labels map[string]string
	Value  float64
}

// Series is one time series from a range query: the labels identifying it and
// its samples, oldest first.
type Series struct {
	Labels map[string]string
	Points []Point
}

// Point is a single sample — a Unix timestamp in seconds and the value at it.
type Point struct {
	Timestamp float64
	Value     float64
}
