package sizing

import "time"

// Category of recommendation.
type Category string

const (
	CategoryOverProvisioned  Category = "over-provisioned"
	CategoryApproachingLimit Category = "approaching-limit"
	CategoryAtLimit          Category = "at-limit"
	CategoryNoLimits         Category = "no-limits"
	CategoryNoReservations   Category = "no-reservations"
)

// Severity for visual treatment.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Recommendation is a single right-sizing hint for a resource.
type Recommendation struct {
	Category   Category `json:"category"`
	Severity   Severity `json:"severity"`
	Resource   string   `json:"resource"`
	Message    string   `json:"message"`
	Current    float64  `json:"current"`
	Configured float64  `json:"configured"`
	Suggested  *float64 `json:"suggested,omitempty"`
}

// ServiceSizing holds all recommendations for a single service.
type ServiceSizing struct {
	ServiceID   string           `json:"serviceId"`
	ServiceName string           `json:"serviceName"`
	Hints       []Recommendation `json:"hints"`
	ComputedAt  time.Time        `json:"computedAt"`
}
