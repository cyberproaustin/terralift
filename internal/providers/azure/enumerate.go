package azure

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the canonical inventory from Azure Resource Graph:
// a KQL floor projection (metadata + the properties bag), then RBAC + exposure
// enrichers. Scope is a subscription; each resource's container is its RG.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	sub := run.Scope.ID
	run.Log.Info("Enumerate", "Azure Resource Graph: subscription=%s", sub)

	const floor = `Resources | project id, name, type, kind, location, resourceGroup, subscriptionId, tags, sku, identity, properties`
	rows, err := graphQuery(ctx, sub, floor)
	if err != nil {
		return nil, err
	}

	inv := &model.Inventory{
		Cloud:       "azure",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   make(map[string]*model.Resource, len(rows)),
		Containers:  map[string]*model.Container{},
	}
	for _, r := range rows {
		res := rowToResource(r)
		if res.ID == "" {
			continue
		}
		inv.Resources[strings.ToLower(res.ID)] = res
	}
	if want := containerSet(run.Config.Containers); want != nil {
		dropped := 0
		for id, res := range inv.Resources {
			if !want[strings.ToLower(res.Container)] {
				delete(inv.Resources, id)
				dropped++
			}
		}
		run.Log.Info("Enumerate", "container filter %v: kept %d, dropped %d", run.Config.Containers, len(inv.Resources), dropped)
	}
	mapped := 0
	for _, res := range inv.Resources {
		if res.TFType != "" {
			mapped++
		}
	}
	run.Log.Info("Enumerate", "floor: %d resources (%d tf-mapped, %d unmapped)", len(inv.Resources), mapped, len(inv.Resources)-mapped)

	if err := enrichIAM(ctx, sub, inv, run); err != nil {
		run.Log.Warn("Enumerate", "RBAC enrichment failed: %v", err)
	}
	if want := containerSet(run.Config.Containers); want != nil {
		filterIAMByContainer(inv, want) // keep subscription-level + in-scope bindings only
	}
	enrichExposure(inv, run)

	inv.Counts.Resources = len(inv.Resources)
	return inv, nil
}

var scopeRGRe = regexp.MustCompile(`(?i)/resourcegroups/([^/]+)`)

// filterIAMByContainer drops role assignments outside the container filter so a
// scoped run's hygiene report doesn't reference resource groups the user
// excluded. Subscription-level bindings (no resourceGroups segment) are kept —
// they apply to the whole subscription, including the targeted groups.
func filterIAMByContainer(inv *model.Inventory, want map[string]bool) {
	kept := inv.IAM[:0]
	for _, b := range inv.IAM {
		m := scopeRGRe.FindStringSubmatch(b.Scope)
		if m == nil || want[strings.ToLower(m[1])] {
			kept = append(kept, b)
		}
	}
	inv.IAM = kept
}

func rowToResource(r map[string]any) *model.Resource {
	id := str(r["id"])
	nativeType := str(r["type"])
	return &model.Resource{
		ID:         id,
		Name:       str(r["name"]),
		NativeType: nativeType,
		TFType:     azureTypeToTFType(nativeType),
		Container:  str(r["resourceGroup"]),
		Location:   str(r["location"]),
		Tags:       toStringMap(r["tags"]),
		Properties: toMap(r["properties"]),
		Source:     "resourcegraph",
	}
}

// enrichIAM pulls RBAC role assignments (+ role definitions for names) via the
// authorizationresources table and joins by scope PREFIX (an Azure assignment
// at a subscription/RG scope applies to every resource beneath it). Resource-
// scoped assignments attach to the resource; all land in inv.IAM for hygiene.
func enrichIAM(ctx context.Context, sub string, inv *model.Inventory, run *core.Run) error {
	assignments, err := graphQuery(ctx, sub, `authorizationresources
| where type =~ 'microsoft.authorization/roleassignments'
| project id, scope=tostring(properties.scope), principalId=tostring(properties.principalId), principalType=tostring(properties.principalType), roleDefinitionId=tolower(tostring(properties.roleDefinitionId))`)
	if err != nil {
		return err
	}
	// Built-in role definitions are not reliably present in the Resource Graph
	// authorizationresources table, so resolve names authoritatively from ARM.
	defs := loadRoleDefinitions(ctx, sub)

	directByRes := map[string][]model.IAMBinding{}
	for _, a := range assignments {
		scope := str(a["scope"])
		name, priv := resolveRole(str(a["roleDefinitionId"]), defs)
		b := model.IAMBinding{
			ID:            str(a["id"]),
			PrincipalID:   str(a["principalId"]),
			PrincipalType: str(a["principalType"]),
			Role:          name,
			Scope:         scope,
			Privileged:    priv,
		}
		scopeLower := strings.ToLower(scope)
		if _, isResource := inv.Resources[scopeLower]; !isResource {
			b.Inherited = true // subscription/RG-scoped, inherited by resources beneath
		}
		inv.IAM = append(inv.IAM, b)
		if !b.Inherited {
			directByRes[scopeLower] = append(directByRes[scopeLower], b)
		}
	}
	for id, res := range inv.Resources {
		res.IAM = append(res.IAM, directByRes[id]...)
	}
	run.Log.Info("Enumerate", "RBAC: %d assignments (%d role definitions resolved)", len(inv.IAM), len(defs))
	return nil
}

// loadRoleDefinitions returns guid(lower) -> {name, privileged} for every role
// definition assignable at the subscription scope (built-in + custom), in a
// single ARM call. Best-effort: on error (e.g. no directory read), returns nil
// and resolveRole falls back to the curated map + name heuristic.
func loadRoleDefinitions(ctx context.Context, sub string) map[string]roleInfo {
	var raw []struct {
		Name     string `json:"name"` // the role-definition GUID
		RoleName string `json:"roleName"`
	}
	if err := runAz(ctx, &raw, "role", "definition", "list", "--scope", "/subscriptions/"+sub); err != nil {
		return nil
	}
	out := make(map[string]roleInfo, len(raw))
	for _, d := range raw {
		out[strings.ToLower(d.Name)] = roleInfo{name: d.RoleName, privileged: inferPrivileged(d.RoleName)}
	}
	return out
}

// enrichExposure derives public-reachability from the properties bag.
func enrichExposure(inv *model.Inventory, run *core.Run) {
	exposed := 0
	for _, res := range inv.Resources {
		e := model.Exposure{}
		p := res.Properties
		pna := strings.ToLower(str(p["publicNetworkAccess"]))
		if pna == "enabled" {
			e.IsPubliclyExposed = true
			e.Notes = append(e.Notes, "publicNetworkAccess=Enabled")
		}
		if acls := toMap(p["networkAcls"]); acls != nil {
			if strings.EqualFold(str(acls["defaultAction"]), "Allow") && pna != "disabled" {
				e.IsPubliclyExposed = true
				e.Notes = append(e.Notes, "networkAcls.defaultAction=Allow")
			}
		}
		if strings.Contains(res.NativeType, "storageaccounts") {
			if abpa, ok := p["allowBlobPublicAccess"].(bool); ok && abpa {
				e.IsPubliclyExposed = true
				e.Notes = append(e.Notes, "allowBlobPublicAccess=true")
			}
		}
		res.Exposure = e
		if e.IsPubliclyExposed {
			exposed++
		}
	}
	run.Log.Info("Enumerate", "exposure: %d publicly-reachable resource(s)", exposed)
}
