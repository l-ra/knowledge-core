package pkgcompat

import (
	"fmt"
)

// BreakingError is returned when a package upgrade is not backward compatible.
type BreakingError struct {
	Findings []Finding
	Context  string
}

func (e *BreakingError) Error() string {
	n := 0
	for _, f := range e.Findings {
		if f.Kind == KindBreaking {
			n++
		}
	}
	if e.Context != "" {
		return fmt.Sprintf("backward-incompatible package change (%s): %d breaking finding(s)", e.Context, n)
	}
	return fmt.Sprintf("backward-incompatible package change: %d breaking finding(s)", n)
}

func NewBreakingError(ctx string, findings []Finding) error {
	br := BreakingFindings(findings)
	if len(br) == 0 {
		return nil
	}
	return &BreakingError{Findings: findings, Context: ctx}
}
