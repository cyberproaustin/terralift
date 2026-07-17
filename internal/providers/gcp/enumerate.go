package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// --- Cloud Asset Inventory JSON shapes (verified against live search output) ---

type caiVersionedResource struct {
	Version  string         `json:"version"`
	Resource map[string]any `json:"resource"`
}

type caiResource struct {
	Name               string                 `json:"name"`
	AssetType          string                 `json:"assetType"`
	Project            string                 `json:"project"`
	Folders            []string               `json:"folders"`
	Location           string                 `json:"location"`
	DisplayName        string                 `json:"displayName"`
	Labels             map[string]string      `json:"labels"`
	VersionedResources []caiVersionedResource `json:"versionedResources"`
}

type caiIam struct {
	Resource string `json:"resource"`
	Policy   struct {
		Bindings []struct {
			Role    string   `json:"role"`
			Members []string `json:"members"`
		} `json:"bindings"`
	} `json:"policy"`
}

// enumerate builds the canonical inventory from Cloud Asset Inventory:
// search-all-resources --read-mask="*" (metadata + full config in one sweep),
// search-all-iam-policies (IAM), then public-exposure signals.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	scope := gcpScope(run.Scope)
	run.Log.Info("Enumerate", "Cloud Asset Inventory search: scope=%s", scope)

	var results []caiResource
	if err := runGcloudJSON(ctx, &results, "asset", "search-all-resources", "--scope="+scope, "--read-mask=*"); err != nil {
		return nil, err
	}

	inv := &model.Inventory{
		Cloud:       "gcp",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   make(map[string]*model.Resource, len(results)),
		Containers:  map[string]*model.Container{},
	}
	mapped, unmapped := 0, 0
	for _, r := range results {
		res := caiToResource(r)
		if res.TFType == "" {
			unmapped++
		} else {
			mapped++
		}
		inv.Resources[strings.ToLower(res.ID)] = res
	}
	run.Log.Info("Enumerate", "floor: %d resources (%d tf-mapped, %d unmapped)", len(inv.Resources), mapped, unmapped)

	if err := enrichIAM(ctx, scope, inv, run); err != nil {
		run.Log.Warn("Enumerate", "IAM enrichment failed: %v", err)
	}
	enrichExposure(inv, run)

	inv.Counts.Resources = len(inv.Resources)
	return inv, nil
}

func caiToResource(r caiResource) *model.Resource {
	var props map[string]any
	if len(r.VersionedResources) > 0 {
		props = r.VersionedResources[0].Resource
	}
	name := r.DisplayName
	if name == "" {
		name = r.Name[strings.LastIndex(r.Name, "/")+1:]
	}
	return &model.Resource{
		ID:         r.Name,
		Name:       name,
		NativeType: r.AssetType,
		TFType:     tfTypeFor(r.AssetType),
		Container:  projectID(r.Project),
		Location:   r.Location,
		Tags:       r.Labels,
		Properties: props,
		Source:     "cai",
	}
}

// enrichIAM runs a single org/folder/project-wide IAM sweep and joins bindings.
// Container-scoped bindings (project/folder/org) are marked inherited and, for a
// single-scope enumeration, apply to every resource beneath that scope; resource-
// scoped bindings attach to their exact resource. All bindings are also kept at
// the inventory level for the hygiene report.
func enrichIAM(ctx context.Context, scope string, inv *model.Inventory, run *core.Run) error {
	var policies []caiIam
	if err := runGcloudJSON(ctx, &policies, "asset", "search-all-iam-policies", "--scope="+scope); err != nil {
		return err
	}
	direct := map[string][]model.IAMBinding{}
	var inherited []model.IAMBinding
	for _, pol := range policies {
		isContainer := strings.Contains(pol.Resource, "cloudresourcemanager.googleapis.com/")
		for _, b := range pol.Policy.Bindings {
			for _, m := range b.Members {
				bind := model.IAMBinding{
					PrincipalID:   m,
					PrincipalType: principalType(m),
					Role:          b.Role,
					Scope:         pol.Resource,
					Privileged:    isPrivilegedRole(b.Role),
					Inherited:     isContainer,
				}
				inv.IAM = append(inv.IAM, bind)
				if isContainer {
					inherited = append(inherited, bind)
				} else {
					direct[strings.ToLower(pol.Resource)] = append(direct[strings.ToLower(pol.Resource)], bind)
				}
			}
		}
	}
	for id, res := range inv.Resources {
		res.IAM = append(res.IAM, direct[id]...)
		res.IAM = append(res.IAM, inherited...) // single-scope: everything inherits container bindings
	}
	run.Log.Info("Enumerate", "IAM: %d bindings (%d container-inherited)", len(inv.IAM), len(inherited))
	return nil
}

// enrichExposure derives public-reachability signals per resource.
func enrichExposure(inv *model.Inventory, run *core.Run) {
	exposed := 0
	for _, res := range inv.Resources {
		e := model.Exposure{}
		// Public IAM (allUsers / allAuthenticatedUsers) attached directly.
		for _, b := range res.IAM {
			if b.PrincipalType == "Public" && !b.Inherited {
				e.IsPubliclyExposed = true
				e.Notes = append(e.Notes, "public IAM member ("+b.PrincipalID+")")
			}
		}
		// Firewall allowing 0.0.0.0/0 ingress.
		if res.NativeType == "compute.googleapis.com/Firewall" {
			if ranges, ok := res.Properties["sourceRanges"].([]any); ok {
				for _, rr := range ranges {
					if fmt.Sprint(rr) == "0.0.0.0/0" {
						e.IsPubliclyExposed = true
						e.Notes = append(e.Notes, "firewall 0.0.0.0/0 ingress")
					}
				}
			}
		}
		res.Exposure = e
		if e.IsPubliclyExposed {
			exposed++
		}
	}
	run.Log.Info("Enumerate", "exposure: %d publicly-reachable resource(s)", exposed)
}

func principalType(member string) string {
	switch {
	case member == "allUsers" || member == "allAuthenticatedUsers":
		return "Public"
	case strings.HasPrefix(member, "user:"):
		return "User"
	case strings.HasPrefix(member, "serviceAccount:"):
		return "ServiceAccount"
	case strings.HasPrefix(member, "group:"):
		return "Group"
	case strings.HasPrefix(member, "domain:"):
		return "Domain"
	default:
		return "Unknown"
	}
}

// isPrivilegedRole flags broadly-privileged GCP predefined roles for hygiene.
func isPrivilegedRole(role string) bool {
	switch role {
	case "roles/owner", "roles/editor",
		"roles/resourcemanager.organizationAdmin",
		"roles/iam.securityAdmin", "roles/iam.organizationRoleAdmin":
		return true
	}
	return strings.Contains(strings.ToLower(role), "admin")
}
