package gcp

import (
	"context"
	"fmt"
	"regexp"
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
		if res.ID == "" { // guard: a blank asset name would collide on the "" key
			continue
		}
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
	// Prefer a clean displayName, but many types (pubsub, secret) set it to the
	// full resource path — fall back to the last path segment in that case.
	name := r.DisplayName
	if name == "" || strings.Contains(name, "/") {
		name = r.Name[strings.LastIndex(r.Name, "/")+1:]
	}
	// A service account's name is its email; use just the account-id (before "@") so
	// the born-correct label is `tlmega_compute`, not the whole …iam.gserviceaccount.com.
	if r.AssetType == "iam.googleapis.com/ServiceAccount" {
		if at := strings.Index(name, "@"); at > 0 {
			name = name[:at]
		}
	}
	tf := tfTypeFor(r.AssetType)
	// Several CAI asset types map to DIFFERENT Terraform resources depending on the
	// resource's scope (global vs regional vs zonal, or project vs billing/folder/org).
	// CAI reports one asset type for all variants, so resolve the concrete type from
	// the location / resource name — otherwise generate-config-out is handed the wrong
	// resource type, its import fails, and the resource is silently dropped to a gap.
	switch r.AssetType {
	case "iam.googleapis.com/Role":
		// project- vs org-scoped custom role — resolve from the resource name.
		tf = customRoleType(r.Name)
	case "compute.googleapis.com/ForwardingRule":
		if strings.EqualFold(r.Location, "global") {
			tf = "google_compute_global_forwarding_rule"
		}
	case "compute.googleapis.com/Address":
		if strings.EqualFold(r.Location, "global") {
			tf = "google_compute_global_address"
		}
	case "compute.googleapis.com/InstanceGroupManager":
		if gcpZoneRe.MatchString(r.Location) { // zonal MIG (the type map defaults to the region variant)
			tf = "google_compute_instance_group_manager"
		}
	// Internal/regional load-balancer components: CAI reports one asset type for the
	// global and regional variants, so a region location selects the region_* resource
	// (the type map defaults to the global variant).
	case "compute.googleapis.com/BackendService":
		if isRegionLocation(r.Location) {
			tf = "google_compute_region_backend_service"
		}
	case "compute.googleapis.com/HealthCheck":
		if isRegionLocation(r.Location) {
			tf = "google_compute_region_health_check"
		}
	case "compute.googleapis.com/UrlMap":
		if isRegionLocation(r.Location) {
			tf = "google_compute_region_url_map"
		}
	case "compute.googleapis.com/TargetHttpProxy":
		if isRegionLocation(r.Location) {
			tf = "google_compute_region_target_http_proxy"
		}
	case "compute.googleapis.com/Disk":
		if isRegionLocation(r.Location) { // a regional PD (the type map defaults to the zonal disk)
			tf = "google_compute_region_disk"
		}
	case "logging.googleapis.com/LogSink":
		tf = logSinkType(r.Name)
	case "cloudfunctions.googleapis.com/Function":
		// 2nd-gen functions (the modern default) are a distinct Terraform resource
		// from 1st-gen; the type map defaults to gen1. A gen2 function also surfaces
		// as a run.googleapis.com/Service backing service — excludedReason drops that
		// twin so the function is managed once, through cloudfunctions2_function.
		if env, _ := props["environment"].(string); env == "GEN_2" {
			tf = "google_cloudfunctions2_function"
		}
	}
	return &model.Resource{
		ID:         r.Name,
		Name:       name,
		NativeType: r.AssetType,
		TFType:     tf,
		Container:  projectID(r.Project),
		Location:   r.Location,
		Tags:       r.Labels,
		Properties: props,
		Source:     "cai",
	}
}

// customRoleType picks the Terraform custom-role resource by the role's scope,
// read from its CAI resource name (…/projects/… vs …/organizations/…). Folder
// scope has no custom-role resource in the provider, so it also falls to project.
func customRoleType(caiName string) string {
	if strings.Contains(caiName, "/organizations/") {
		return "google_organization_iam_custom_role"
	}
	return "google_project_iam_custom_role"
}

// gcpZoneRe matches a zone (region + "-" + a single letter, e.g. us-central1-a),
// distinguishing zonal resources from regional ones (us-central1) by location.
var gcpZoneRe = regexp.MustCompile(`-[a-z]$`)

// isRegionLocation reports whether a CAI location is a REGION (e.g. us-central1),
// as opposed to "global" or a zone (us-central1-a). Used to pick the region_*
// Terraform resource for compute asset types CAI reports without that distinction.
func isRegionLocation(loc string) bool {
	return loc != "" && !strings.EqualFold(loc, "global") && !gcpZoneRe.MatchString(loc)
}

// logSinkType resolves a logging sink's Terraform resource from the scope encoded
// in its CAI resource name. A project sink miswired to google_logging_billing_
// account_sink imports "cleanly" but carries the project NUMBER as a bogus
// billing_account and would drive the wrong API on any real change.
func logSinkType(caiName string) string {
	switch {
	case strings.Contains(caiName, "/billingAccounts/"):
		return "google_logging_billing_account_sink"
	case strings.Contains(caiName, "/folders/"):
		return "google_logging_folder_sink"
	case strings.Contains(caiName, "/organizations/"):
		return "google_logging_organization_sink"
	default:
		return "google_logging_project_sink"
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
	// Attach only RESOURCE-scoped bindings per resource. Container-scoped
	// (project/folder/org) bindings live at the inventory level (inv.IAM) with
	// their real Scope + Inherited flag, so consumers can resolve ancestry
	// correctly — attaching all container bindings to every resource would be
	// wrong for folder/org scope and needlessly bloat the inventory.
	for id, res := range inv.Resources {
		res.IAM = append(res.IAM, direct[id]...)
	}
	run.Log.Info("Enumerate", "IAM: %d bindings (%d container-scoped)", len(inv.IAM), len(inherited))
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
		// Firewall allowing world ingress. Only an ENABLED, INGRESS, ALLOW rule
		// with an open source range is exposure — a disabled rule or a DENY from
		// 0.0.0.0/0 (a protection) is not.
		if res.NativeType == "compute.googleapis.com/Firewall" {
			disabled, _ := res.Properties["disabled"].(bool)
			direction, _ := res.Properties["direction"].(string)
			_, hasAllow := res.Properties["allowed"]
			ingress := direction == "" || strings.EqualFold(direction, "INGRESS")
			if !disabled && hasAllow && ingress {
				if ranges, ok := res.Properties["sourceRanges"].([]any); ok {
					for _, rr := range ranges {
						if s := fmt.Sprint(rr); s == "0.0.0.0/0" || s == "::/0" {
							e.IsPubliclyExposed = true
							e.Notes = append(e.Notes, "firewall "+s+" ingress")
						}
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
