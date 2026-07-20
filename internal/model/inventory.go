// Package model defines the CLOUD-NEUTRAL types that every provider's enumerator
// produces and every shared phase (reconcile/correctness/package) consumes.
// This inventory shape IS the interface between the per-cloud code and the
// provider-agnostic core.
package model

import "time"

// ScopeType is a cloud's enumeration/isolation boundary. It is an open string,
// not a closed enum: a provider declares its own scope type. The constants below
// are the ones the built-in clouds use; a flat provider (a SaaS tenant with no
// sub-scoping) uses ScopeTenant or ScopeGlobal.
type ScopeType string

const (
	ScopeProject      ScopeType = "project"      // GCP project
	ScopeFolder       ScopeType = "folder"       // GCP folder
	ScopeOrganization ScopeType = "organization" // GCP org
	ScopeSubscription ScopeType = "subscription" // Azure subscription
	ScopeAccount      ScopeType = "account"      // AWS account
	ScopeTenant       ScopeType = "tenant"       // flat SaaS/platform tenant (the whole org is the scope)
	ScopeGlobal       ScopeType = "global"       // no meaningful sub-scoping
)

// Scope is a normalized enumeration target, e.g. {project, my-proj-id}.
type Scope struct {
	Type ScopeType `json:"type"`
	ID   string    `json:"id"`
}

// Inventory is the SOURCE OF TRUTH FOR REALITY — built by a provider's Enumerate,
// consumed unchanged by the shared reconcile/correctness/package phases.
type Inventory struct {
	Cloud       string                `json:"cloud"`
	Scope       Scope                 `json:"scope"`
	GeneratedAt time.Time             `json:"generatedAt"`
	Resources   map[string]*Resource  `json:"resources"`     // keyed by lower-cased canonical ID
	Containers  map[string]*Container `json:"containers"`    // projects / resource groups / accounts
	Hierarchy   []*HierarchyNode      `json:"hierarchy"`     // org/folder/project (GCP) or MG tree (Azure)
	IAM         []IAMBinding          `json:"iam,omitempty"` // all bindings (direct + container-inherited) for the hygiene report
	Counts      Counts                `json:"counts"`
}

type Counts struct {
	Resources      int `json:"resources"`
	Containers     int `json:"containers"`
	SkippedByScope int `json:"skippedByScope"`
	DeepDetail     int `json:"deepDetail"`
}

// Resource is one control-plane resource in cloud-neutral form.
type Resource struct {
	ID         string            `json:"id"` // canonical cloud ID (Azure resourceId / GCP asset name / AWS ARN)
	Name       string            `json:"name"`
	NativeType string            `json:"nativeType"` // e.g. compute.googleapis.com/Instance
	TFType     string            `json:"tfType"`     // e.g. google_compute_instance (filled at export/reconcile)
	Container  string            `json:"container"`  // project / resource group / account
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags"`       // GCP labels map in here
	Properties map[string]any    `json:"properties"` // full config bag (versionedResources / az rest / describe)
	IAM        []IAMBinding      `json:"iam"`
	Exposure   Exposure          `json:"exposure"`
	Source     string            `json:"source"` // enumeration rung: graph|rest|cai
}

// Container is a scope-holding node (GCP project, Azure resource group, AWS account).
type Container struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Type   ScopeType         `json:"type"`
	Parent string            `json:"parent"`
	Tags   map[string]string `json:"tags"`
}

// HierarchyNode models org/folder/project (GCP) or management-group trees (Azure).
type HierarchyNode struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Type     ScopeType        `json:"type"`
	Parent   string           `json:"parent"`
	Children []*HierarchyNode `json:"children,omitempty"`
}

// IAMBinding is a normalized access grant: Azure role assignment, GCP IAM binding,
// AWS policy statement. Inherited=true means it came from an ancestor in the
// hierarchy (GCP org/folder inheritance) rather than being attached directly.
type IAMBinding struct {
	ID            string `json:"id,omitempty"` // native binding id (e.g. Azure roleAssignments/<guid>); import id
	PrincipalID   string `json:"principalId"`
	PrincipalType string `json:"principalType"`    // User | Group | ServiceAccount/ServicePrincipal
	Role          string `json:"role"`             // human-readable role name when resolvable, else the raw id/guid
	RoleID        string `json:"roleId,omitempty"` // canonical role-definition id (Azure: full roleDefinitions/<guid> path) — authoring fallback
	Scope         string `json:"scope"`            // the resource/hierarchy node the grant is attached to
	Privileged    bool   `json:"privileged"`
	Inherited     bool   `json:"inherited"`
}

// Exposure holds public-reachability signals for the hygiene/lockdown report.
type Exposure struct {
	PublicNetworkAccess string   `json:"publicNetworkAccess,omitempty"`
	IsPubliclyExposed   bool     `json:"isPubliclyExposed"`
	Notes               []string `json:"notes,omitempty"`
}
