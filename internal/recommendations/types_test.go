package recommendations

import "testing"

func TestComputeSummary(t *testing.T) {
	recs := []Recommendation{
		{Severity: SeverityCritical},
		{Severity: SeverityCritical},
		{Severity: SeverityWarning},
		{Severity: SeverityInfo},
	}
	s := ComputeSummary(recs)
	if s.Critical != 2 {
		t.Errorf("critical: got %d, want 2", s.Critical)
	}
	if s.Warning != 1 {
		t.Errorf("warning: got %d, want 1", s.Warning)
	}
	if s.Info != 1 {
		t.Errorf("info: got %d, want 1", s.Info)
	}
}

func TestComputeSummary_Empty(t *testing.T) {
	s := ComputeSummary(nil)
	if s.Critical != 0 || s.Warning != 0 || s.Info != 0 {
		t.Errorf("expected all zeros, got %+v", s)
	}
}
