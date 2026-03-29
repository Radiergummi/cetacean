package recommendations

type Category string

const (
	// Sizing
	CategoryOverProvisioned  Category = "over-provisioned"
	CategoryApproachingLimit Category = "approaching-limit"
	CategoryAtLimit          Category = "at-limit"
	CategoryNoLimits         Category = "no-limits"
	CategoryNoReservations   Category = "no-reservations"

	// Config hygiene
	CategoryNoHealthcheck   Category = "no-healthcheck"
	CategoryNoRestartPolicy Category = "no-restart-policy"

	// Operational
	CategoryFlakyService    Category = "flaky-service"
	CategoryNodeDiskFull    Category = "node-disk-full"
	CategoryNodeMemPressure Category = "node-memory-pressure"

	// Cluster
	CategorySingleReplica       Category = "single-replica"
	CategoryManagerHasWorkloads Category = "manager-has-workloads"
	CategoryUnevenDistribution  Category = "uneven-distribution"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type Scope string

const (
	ScopeService Scope = "service"
	ScopeNode    Scope = "node"
	ScopeCluster Scope = "cluster"
)

type Recommendation struct {
	Category   Category `json:"category"`
	Severity   Severity `json:"severity"`
	Scope      Scope    `json:"scope"`
	TargetID   string   `json:"targetId"`
	TargetName string   `json:"targetName"`
	Resource   string   `json:"resource"`
	Message    string   `json:"message"`
	Current    float64  `json:"current"`
	Configured float64  `json:"configured"`
	Suggested  *float64 `json:"suggested,omitempty"`
	FixAction  *string  `json:"fixAction,omitempty"`
}

type Summary struct {
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
}

// ComputeSummary counts recommendations by severity.
func ComputeSummary(recs []Recommendation) Summary {
	var s Summary
	for _, r := range recs {
		switch r.Severity {
		case SeverityCritical:
			s.Critical++
		case SeverityWarning:
			s.Warning++
		case SeverityInfo:
			s.Info++
		}
	}
	return s
}
