package azure

import (
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/model"
)

func TestGenerateRoleAssignments(t *testing.T) {
	sub := "/subscriptions/s"
	rgScope := sub + "/resourceGroups/rg-a"
	inv := &model.Inventory{IAM: []model.IAMBinding{
		{ID: rgScope + "/.../roleAssignments/g1", Scope: rgScope, Role: "Reader", PrincipalID: "1111-2222"},                              // in-RG -> author
		{ID: rgScope + "/providers/x/roleAssignments/g2", Scope: rgScope + "/providers/x", Role: "Owner", PrincipalID: "3333-4444"},      // resource-in-RG -> author
		{ID: sub + "/.../roleAssignments/g3", Scope: sub, Role: "Owner", PrincipalID: "5555"},                                            // subscription-level -> skip
		{Scope: rgScope, Role: "Reader", PrincipalID: "6666"},                                                                            // no ID -> skip
		{ID: sub + "/resourceGroups/rg-b/.../roleAssignments/g5", Scope: sub + "/resourceGroups/rg-b", Role: "Reader", PrincipalID: "7"}, // other RG -> skip
		{ID: rgScope + "/.../roleAssignments/g1", Scope: rgScope, Role: "Reader", PrincipalID: "1111-2222"},                              // dup ID -> skip
	}}
	hcl, n := generateRoleAssignments(inv, "rg-a", nil)
	if n != 2 {
		t.Fatalf("authored %d, want 2:\n%s", n, hcl)
	}
	if strings.Count(hcl, "resource \"azurerm_role_assignment\"") != 2 {
		t.Errorf("want 2 resource blocks:\n%s", hcl)
	}
	if strings.Count(hcl, "import {") != 2 {
		t.Errorf("want 2 import blocks:\n%s", hcl)
	}
	// Each authored binding's role name must appear.
	if !strings.Contains(hcl, `role_definition_name = "Reader"`) || !strings.Contains(hcl, `role_definition_name = "Owner"`) {
		t.Errorf("missing role_definition_name:\n%s", hcl)
	}
}

func TestGenerateRoleAssignmentsEmpty(t *testing.T) {
	inv := &model.Inventory{IAM: []model.IAMBinding{
		{ID: "/subscriptions/s/.../roleAssignments/g", Scope: "/subscriptions/s", Role: "Owner", PrincipalID: "x"}, // subscription-level only
	}}
	if hcl, n := generateRoleAssignments(inv, "rg-a", nil); n != 0 || hcl != "" {
		t.Errorf("expected no assignments, got %d:\n%s", n, hcl)
	}
}

func TestGenerateRoleAssignmentsScopeRewireAndEscape(t *testing.T) {
	rg := "/subscriptions/s/resourceGroups/rg-a"
	kv := rg + "/providers/Microsoft.KeyVault/vaults/kv1"
	inv := &model.Inventory{IAM: []model.IAMBinding{
		// scope is a managed resource -> should rewire to <addr>.id, not a literal.
		{ID: kv + "/providers/Microsoft.Authorization/roleAssignments/g1", Scope: kv, Role: "Reader", PrincipalID: "p1"},
		// hostile custom-role name must be template-escaped, not left interpolatable.
		{ID: rg + "/providers/Microsoft.Authorization/roleAssignments/g2", Scope: rg, Role: "Evil ${var.x}", PrincipalID: "p2"},
	}}
	addrByID := map[string]string{kv: "azurerm_key_vault.kv1"}
	hcl, n := generateRoleAssignments(inv, "rg-a", addrByID)
	if n != 2 {
		t.Fatalf("n=%d\n%s", n, hcl)
	}
	if !strings.Contains(hcl, "scope                = azurerm_key_vault.kv1.id") {
		t.Errorf("scope not rewired to managed resource:\n%s", hcl)
	}
	if !strings.Contains(hcl, `role_definition_name = "Evil $${var.x}"`) {
		t.Errorf("interpolation not escaped:\n%s", hcl)
	}
	if strings.Contains(hcl, `"Evil ${var.x}"`) {
		t.Errorf("raw interpolation leaked into HCL:\n%s", hcl)
	}
}

func TestRoleAssignmentName(t *testing.T) {
	// Name derives from the assignment's OWN guid (stable), not the principal.
	b := model.IAMBinding{
		ID:          "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Authorization/roleAssignments/92a7f016-c2f2-47e0-81bd-db79ac1df1bb",
		Role:        "Key Vault Secrets Officer",
		PrincipalID: "644545ee-1b6e-4559",
	}
	if got := roleAssignmentName(b); got != "key_vault_secrets_officer_92a7f016" {
		t.Errorf("roleAssignmentName = %q", got)
	}
}
