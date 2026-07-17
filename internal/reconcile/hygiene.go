package reconcile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cyberproaustin/terralift/internal/model"
)

// HygieneFinding is one lockdown item.
type HygieneFinding struct {
	Kind     string `json:"kind"` // privileged-human | public-exposure | public-iam-scope
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
// Deterministic (sorted) so re-runs diff cleanly. Catches container-scoped public
// grants (project/folder/org allUsers) that per-resource exposure can't see.
func Hygiene(inv *model.Inventory) HygieneReport {
	rep := HygieneReport{}

	bindings := append([]model.IAMBinding(nil), inv.IAM...)
	sort.Slice(bindings, func(i, j int) bool {
		a, b := bindings[i], bindings[j]
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		if a.Role != b.Role {
			return a.Role < b.Role
		}
		return a.PrincipalID < b.PrincipalID
	})
	seenPriv := map[string]bool{}
	for _, b := range bindings {
		if b.Privileged {
			key := b.PrincipalID + "|" + b.Role + "|" + b.Scope
			if !seenPriv[key] {
				seenPriv[key] = true
				rep.PrivilegedBindings++
				if b.PrincipalType == "User" {
					rep.HumanPrivileged++
					rep.Findings = append(rep.Findings, HygieneFinding{
						Kind: "privileged-human", Detail: b.Role + " -> " + b.PrincipalID, Resource: b.Scope,
					})
				}
			}
		}
		// Container-scoped public grant (project/folder/org allUsers) — per-resource
		// exposure can't observe this, so surface it here.
		if b.Inherited && b.PrincipalType == "Public" {
			rep.Findings = append(rep.Findings, HygieneFinding{
				Kind: "public-iam-scope", Detail: b.Role + " -> " + b.PrincipalID, Resource: b.Scope,
			})
		}
	}

	// Resource-level exposure (allUsers on a resource, open firewall, ...), sorted.
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
