package reconcile

import (
	"math"
	"sort"
	"strings"
)

// MissingResource is one enumerated resource absent from the export (a gap).
type MissingResource struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Container string `json:"container"`
	// TFType is the Terraform type resolved during enumeration. EMPTY means there is
	// genuinely no mapping (an unsupported type). NON-EMPTY means we knew how to
	// onboard it and the EXPORT failed — most often because the calling principal
	// lacks a required action (e.g. Reader cannot call
	// Microsoft.Storage/storageAccounts/listKeys/action, so the provider read 403s and
	// the resource never reaches the generated HCL). Reporting both as "unsupported
	// type" hides a permissions problem behind a coverage number.
	TFType string `json:"tfType,omitempty"`
}

// CoverageReport is the set-diff oracle. It separates INTENTIONAL exclusions
// (GCP-managed defaults, sub-resources, noise) from genuine GAPS (unsupported
// types), so the coverage % is not dragged down by things we deliberately skip.
type CoverageReport struct {
	Enumerated  int               `json:"enumerated"`
	Considered  int               `json:"considered"` // enumerated - excluded (the honest denominator)
	Covered     int               `json:"covered"`
	Excluded    int               `json:"excluded"`
	Gap         int               `json:"gap"`
	CoveragePct float64           `json:"coveragePercent"` // covered / considered
	Missing     []MissingResource `json:"missing"`         // the gap detail (sorted)
}

// Coverage computes the gap: enumerated IDs that were neither exported nor
// intentionally excluded. Coverage % = covered / (enumerated − excluded).
// IDs are compared case-insensitively; Missing is sorted for stable diffs.
func Coverage(enumeratedIDs, exportedIDs, excludedIDs []string, meta map[string]MissingResource) CoverageReport {
	exp := toSet(exportedIDs)
	excl := toSet(excludedIDs)
	// Defend the lowercase-key contract locally instead of trusting the caller, so a
	// mixed-case meta key can't silently drop a gap's Type/Name detail from the report.
	metaLower := make(map[string]MissingResource, len(meta))
	for k, v := range meta {
		metaLower[strings.ToLower(k)] = v
	}
	meta = metaLower

	var missing []MissingResource
	excluded := 0
	for _, id := range enumeratedIDs {
		l := strings.ToLower(id)
		if excl[l] {
			excluded++
			continue
		}
		if exp[l] {
			continue
		}
		if m, ok := meta[l]; ok {
			missing = append(missing, m)
		} else {
			missing = append(missing, MissingResource{ID: id})
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].ID < missing[j].ID })

	enum := len(enumeratedIDs)
	considered := enum - excluded
	covered := considered - len(missing)
	pct := 0.0
	if considered > 0 {
		pct = math.Round(1000*float64(covered)/float64(considered)) / 10
	}
	return CoverageReport{
		Enumerated: enum, Considered: considered, Covered: covered,
		Excluded: excluded, Gap: len(missing), CoveragePct: pct, Missing: missing,
	}
}

func toSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[strings.ToLower(id)] = true
	}
	return m
}
