package reconcile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cyberproaustin/terralift/internal/model"
)

// HygieneFinding is one lockdown item.
type HygieneFinding struct {
	Kind     string `json:"kind"` // privileged-human | public-exposure
	Detail   string `json:"detail"`
	Resource string `json:"resource"`
}

// HygieneReport is the pre-pipeline lockdown assessment.
type HygieneReport struct {
	PrivilegedBindings int              `json:"privilegedBindings"`
	HumanPrivileged    int              `json:"humanPrivileged"`
	PubliclyExposed    int              `json:"publiclyExposed"`
	Findings           []HygieneFinding `json:"findings"`
	Actions            []string         `json:"actions"`
}

// Hygiene derives the lockdown report from the inventory's IAM + exposure.
// Cloud-agnostic: reads model.IAMBinding.Privileged / PrincipalType and
// model.Exposure, which each provider populates in its own terms.
func Hygiene(inv *model.Inventory) HygieneReport {
	rep := HygieneReport{}
	seen := map[string]bool{}
	for _, b := range inv.IAM {
		if !b.Privileged {
			continue
		}
		key := b.PrincipalID + "|" + b.Role + "|" + b.Scope
		if seen[key] {
			continue
		}
		seen[key] = true
		rep.PrivilegedBindings++
		if b.PrincipalType == "User" {
			rep.HumanPrivileged++
			rep.Findings = append(rep.Findings, HygieneFinding{
				Kind: "privileged-human", Detail: b.Role + " -> " + b.PrincipalID, Resource: b.Scope,
			})
		}
	}
	ids := make([]string, 0, len(inv.Resources))
	for id := range inv.Resources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := inv.Resources[id]
		if r.Exposure.IsPubliclyExposed {
			rep.PubliclyExposed++
			rep.Findings = append(rep.Findings, HygieneFinding{
				Kind: "public-exposure", Detail: strings.Join(r.Exposure.Notes, ", "), Resource: r.ID,
			})
		}
	}
	if rep.HumanPrivileged > 0 {
		rep.Actions = append(rep.Actions, fmt.Sprintf("Demote %d human privileged binding(s) to a read role; let the pipeline identity own changes.", rep.HumanPrivileged))
	}
	if rep.PubliclyExposed > 0 {
		rep.Actions = append(rep.Actions, fmt.Sprintf("Review %d publicly-exposed resource(s) (allUsers/allAuthenticatedUsers IAM, 0.0.0.0/0 firewalls).", rep.PubliclyExposed))
	}
	return rep
}
