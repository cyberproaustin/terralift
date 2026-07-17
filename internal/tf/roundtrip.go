// Package tf holds Terraform-facing helpers: pure parsers for plan/schema JSON
// now, and (later) a driver wrapping hashicorp/terraform-exec.
package tf

import "encoding/json"

// RoundTrip is the parsed correctness-oracle result of a terraform plan.
type RoundTrip struct {
	Clean []string    // addresses that round-tripped clean (no-op)
	Drift []DriftItem // addresses with residual drift (a property not captured)
}

// DriftItem is one resource that did not no-op.
type DriftItem struct {
	Address string
	Actions []string
}

// ParseRoundTrip parses `terraform show -json <plan>` output. A managed resource
// whose change.actions == ["no-op"] round-tripped clean; anything else is drift.
// Data sources (mode != "managed") are ignored.
// Source: https://developer.hashicorp.com/terraform/internals/json-format
func ParseRoundTrip(planJSON []byte) (*RoundTrip, error) {
	var plan struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Mode    string `json:"mode"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		return nil, err
	}
	rt := &RoundTrip{}
	for _, rc := range plan.ResourceChanges {
		if rc.Mode != "" && rc.Mode != "managed" {
			continue
		}
		a := rc.Change.Actions
		if len(a) == 1 && a[0] == "no-op" {
			rt.Clean = append(rt.Clean, rc.Address)
		} else {
			rt.Drift = append(rt.Drift, DriftItem{Address: rc.Address, Actions: a})
		}
	}
	return rt, nil
}
