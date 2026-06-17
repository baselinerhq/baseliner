package models

// Severity classifies the importance of a check. Weights drive the
// severity-weighted repo score in the policy engine.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// SeverityWeight mirrors the Python SEVERITY_WEIGHT map.
var SeverityWeight = map[Severity]int{
	SeverityCritical: 4,
	SeverityHigh:     3,
	SeverityMedium:   2,
	SeverityLow:      1,
}

// Weight returns the scoring weight for a severity, defaulting to 1 for
// unknown values (matching the engine's fallback behavior).
func (s Severity) Weight() int {
	if w, ok := SeverityWeight[s]; ok {
		return w
	}
	return 1
}
