// Package prometheus talks to a Prometheus server: Client runs instant and
// range queries, Proxy forwards the dashboard's own query traffic.
//
// It sits beside internal/api rather than inside it because neither half is a
// REST concern. Client is the concrete type behind internal/mcp's
// MetricsQuerier, which main wires; Proxy reaches back into the transport for
// error rendering through an ErrorWriter rather than importing it, which is
// what lets the package stand alone.
//
// The query result types live in internal/prom, so a caller that only reads
// series — internal/recommendations, internal/mcp — does not pull in an HTTP
// client to name them.
package prometheus
