// Package prometheus talks to a Prometheus server: Client runs instant and range
// queries, Proxy forwards the dashboard's query traffic. Proxy renders errors
// through an ErrorWriter rather than importing the transport. The query result
// types live in internal/prom, so a series reader pulls in no HTTP client.
package prometheus
