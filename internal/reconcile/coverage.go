package reconcile

import (
	"math"
	"strings"
)

// MissingResource is one enumerated resource absent from the export (a gap).
type MissingResource struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Container string `json:"container"`
}

// CoverageReport is the set-diff oracle: enumerated vs exported.
type CoverageReport struct {
	Enumerated    int               `json:"enumerated"`
	Exported      int               `json:"exported"`      // total in export (may exceed enumerated)
	Covered       int               `json:"covered"`       // enumerated resources that were exported
	ExtraExported int               `json:"extraExported"` // child/implicit resources beyond the floor
	CoveragePct   float64           `json:"coveragePercent"`
	Missing       []MissingResource `json:"missing"`
}

// Coverage computes the gap: enumerated IDs not present in the exported set.
// Coverage % is covered/enumerated (NOT exported/enumerated) — the exporter is
// a SUPERSET of the enumeration floor (it also captures child/implicit resources
// and containers), so exported can exceed enumerated; those extras are reported
// separately, never as >100%. IDs are compared case-insensitively.
func Coverage(enumeratedIDs, exportedIDs []string, meta map[string]MissingResource) CoverageReport {
	exp := make(map[string]bool, len(exportedIDs))
	for _, id := range exportedIDs {
		exp[strings.ToLower(id)] = true
	}
	var missing []MissingResource
	for _, id := range enumeratedIDs {
		if !exp[strings.ToLower(id)] {
			if m, ok := meta[strings.ToLower(id)]; ok {
				missing = append(missing, m)
			} else {
				missing = append(missing, MissingResource{ID: id})
			}
		}
	}
	enum := len(enumeratedIDs)
	covered := enum - len(missing)
	extra := len(exp) - covered
	if extra < 0 {
		extra = 0
	}
	pct := 0.0
	if enum > 0 {
		pct = math.Round(1000*float64(covered)/float64(enum)) / 10
	}
	return CoverageReport{
		Enumerated: enum, Exported: len(exp), Covered: covered,
		ExtraExported: extra, CoveragePct: pct, Missing: missing,
	}
}
