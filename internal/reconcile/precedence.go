// Package reconcile holds the shared, cloud-agnostic reconciliation logic
// (Phases 4-6): coverage, reference rewire, precedence, migration, layout.
package reconcile

import "fmt"

// PrecedenceInput carries the inputs for one property's precedence decision.
type PrecedenceInput struct {
	Property          string
	InPlay            bool // provider schema says writable?
	HasProviderSchema bool // does the provider model this resource type at all?
	ExportValue       any
	TruthValue        any // REST/describe/CAI "truth"
	ExportAPIVersion  string
	TruthAPIVersion   string
}

// Decision is a precedence-engine outcome.
type Decision struct {
	Property string
	Decision string // drop | route-to-raw | skip-version-skew | correct-to-truth | agree
	Reason   string
}

// ResolvePrecedence applies the mandated ordering when sources disagree:
//  1. provider SCHEMA gates whether a field is in play (writable). If the type
//     isn't modeled at all, route to a raw-API provider (AzAPI / google raw / awscc)
//     rather than dropping; if modeled but the attr is read-only, drop it.
//  2. if in play and export disagrees with truth, TRUTH is the value tiebreaker.
//  3. `terraform plan` is the final arbiter (Phase 5), regardless of 1-2.
//
// Version skew is normalized first: differing api-versions are NOT a real
// conflict (clouds without api-versions, e.g. GCP, never hit this branch).
func ResolvePrecedence(in PrecedenceInput) Decision {
	if !in.HasProviderSchema {
		return Decision{in.Property, "route-to-raw", "provider does not model this resource type"}
	}
	if !in.InPlay {
		return Decision{in.Property, "drop", "attribute is read-only/computed in provider schema"}
	}
	if in.ExportAPIVersion != "" && in.TruthAPIVersion != "" && in.ExportAPIVersion != in.TruthAPIVersion {
		return Decision{in.Property, "skip-version-skew", "api-version skew; not a real conflict"}
	}
	if !valueEqual(in.ExportValue, in.TruthValue) {
		return Decision{in.Property, "correct-to-truth", "export value disagrees with truth"}
	}
	return Decision{in.Property, "agree", "export and truth agree"}
}

// valueEqual is structural equality good enough for precedence (scalars + maps).
func valueEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		if len(am) != len(bm) {
			return false
		}
		for k, av := range am {
			bv, ok := bm[k]
			if !ok || !valueEqual(av, bv) {
				return false
			}
		}
		return true
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}
