package azure

import (
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

func TestAzureTypeToTFType(t *testing.T) {
	cases := map[string]string{
		"microsoft.storage/storageAccounts": "azurerm_storage_account", // mixed case in -> lowered
		"Microsoft.KeyVault/vaults":         "azurerm_key_vault",
		"microsoft.sql/servers/databases":   "azurerm_mssql_database",
		"microsoft.unknown/thing":           "",
	}
	for in, want := range cases {
		if got := azureTypeToTFType(in); got != want {
			t.Errorf("azureTypeToTFType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInferPrivileged(t *testing.T) {
	for _, n := range []string{"Owner", "Contributor", "User Access Administrator", "Some Administrator", "admin"} {
		if !inferPrivileged(n) {
			t.Errorf("inferPrivileged(%q) should be true", n)
		}
	}
	for _, n := range []string{"Reader", "Storage Blob Data Contributor", "Key Vault Secrets User", ""} {
		// "Storage Blob Data Contributor" DOES contain "contributor" -> heuristic true.
		if n == "Storage Blob Data Contributor" {
			continue
		}
		if inferPrivileged(n) {
			t.Errorf("inferPrivileged(%q) should be false", n)
		}
	}
}

func TestResolveRolePrecedence(t *testing.T) {
	full := "/subscriptions/s/providers/Microsoft.Authorization/roleDefinitions/8e3af657-a8ff-443c-a75c-2fe8c4bcb635"
	// 1) curated builtinRoles wins even if defs disagree.
	defs := map[string]roleInfo{
		"8e3af657-a8ff-443c-a75c-2fe8c4bcb635": {"Bogus", false},
		"11111111-1111-1111-1111-111111111111": {"Custom Contributor", true},
	}
	if name, priv := resolveRole(full, defs); name != "Owner" || !priv {
		t.Errorf("curated should win: got %q priv=%v", name, priv)
	}
	// 2) az-listed defs used when not curated.
	if name, priv := resolveRole("11111111-1111-1111-1111-111111111111", defs); name != "Custom Contributor" || !priv {
		t.Errorf("defs lookup: got %q priv=%v", name, priv)
	}
	// 3) fallback to guid + name heuristic when unknown everywhere.
	if name, priv := resolveRole("deadbeef-0000-0000-0000-000000000000", nil); name != "deadbeef-0000-0000-0000-000000000000" || priv {
		t.Errorf("fallback: got %q priv=%v", name, priv)
	}
}

func TestRowToResource(t *testing.T) {
	row := map[string]any{
		"id":            "/subscriptions/s/resourceGroups/RG1/providers/Microsoft.Storage/storageAccounts/acct",
		"name":          "acct",
		"type":          "microsoft.storage/storageaccounts",
		"resourceGroup": "rg1",
		"location":      "eastus2",
		"tags":          map[string]any{"env": "dev", "n": 1},
		"properties":    map[string]any{"allowBlobPublicAccess": true},
	}
	r := rowToResource(row)
	if r.TFType != "azurerm_storage_account" {
		t.Errorf("TFType = %q", r.TFType)
	}
	if r.Container != "rg1" || r.Location != "eastus2" || r.Name != "acct" {
		t.Errorf("bad mapping: %+v", r)
	}
	if r.Tags["env"] != "dev" || r.Tags["n"] != "" { // non-string tag value -> ""
		t.Errorf("tags = %v", r.Tags)
	}
	if r.Source != "resourcegraph" {
		t.Errorf("source = %q", r.Source)
	}
}

func TestEnrichExposure(t *testing.T) {
	inv := &model.Inventory{Resources: map[string]*model.Resource{
		"a": {NativeType: "microsoft.storage/storageaccounts", Properties: map[string]any{"allowBlobPublicAccess": true, "publicNetworkAccess": "Disabled"}},
		"b": {NativeType: "microsoft.keyvault/vaults", Properties: map[string]any{"publicNetworkAccess": "Enabled"}},
		"c": {NativeType: "microsoft.storage/storageaccounts", Properties: map[string]any{"publicNetworkAccess": "Disabled", "networkAcls": map[string]any{"defaultAction": "Allow"}}},
		"d": {NativeType: "microsoft.sql/servers", Properties: map[string]any{"publicNetworkAccess": "Disabled"}},
	}}
	enrichExposure(inv, quietRun())
	if !inv.Resources["a"].Exposure.IsPubliclyExposed {
		t.Error("a: allowBlobPublicAccess=true should be exposed")
	}
	if !inv.Resources["b"].Exposure.IsPubliclyExposed {
		t.Error("b: publicNetworkAccess=Enabled should be exposed")
	}
	if inv.Resources["c"].Exposure.IsPubliclyExposed {
		t.Error("c: defaultAction=Allow but publicNetworkAccess=Disabled should NOT be exposed")
	}
	if inv.Resources["d"].Exposure.IsPubliclyExposed {
		t.Error("d: fully private should NOT be exposed")
	}
}

func TestFilterIAMByContainer(t *testing.T) {
	sub := "/subscriptions/s"
	inv := &model.Inventory{IAM: []model.IAMBinding{
		{Role: "Owner", Scope: sub},                                          // subscription-level -> keep
		{Role: "Reader", Scope: sub + "/resourceGroups/rg-keep"},             // in-scope -> keep
		{Role: "Reader", Scope: sub + "/resourceGroups/rg-keep/providers/x"}, // resource in-scope -> keep
		{Role: "Reader", Scope: sub + "/resourceGroups/rg-other"},            // out of scope -> drop
		{Role: "Reader", Scope: sub + "/resourceGroups/RG-KEEP/providers/y"}, // case-insensitive -> keep
	}}
	filterIAMByContainer(inv, containerSet([]string{"rg-keep"}))
	if len(inv.IAM) != 4 {
		t.Fatalf("kept %d bindings, want 4: %+v", len(inv.IAM), inv.IAM)
	}
	for _, b := range inv.IAM {
		if scopeRGRe.MatchString(b.Scope) && !containerSet([]string{"rg-keep"})[strings.ToLower(scopeRGRe.FindStringSubmatch(b.Scope)[1])] {
			t.Errorf("kept out-of-scope binding: %s", b.Scope)
		}
	}
}

func quietRun() *core.Run {
	return &core.Run{Log: core.NewLogger(core.LevelError)}
}

// Microsoft.Web/sites is one ARM type covering four TF resources; only `kind`
// distinguishes Windows/Linux and web/function. Guessing Linux mislabels every
// Windows app (the real-world bug: azurerm_windows_web_app reported as a gap).
func TestAzureTypeToTFTypeKind(t *testing.T) {
	cases := []struct{ typ, kind, want string }{
		{"microsoft.web/sites", "app", "azurerm_windows_web_app"},
		{"microsoft.web/sites", "app,linux", "azurerm_linux_web_app"},
		{"microsoft.web/sites", "functionapp", "azurerm_windows_function_app"},
		{"microsoft.web/sites", "functionapp,linux", "azurerm_linux_function_app"},
		{"microsoft.web/sites", "app,linux,container", "azurerm_linux_web_app"},
		{"microsoft.web/sites/slots", "app", "azurerm_windows_web_app_slot"},
		{"microsoft.web/sites/slots", "app,linux", "azurerm_linux_web_app_slot"},
		{"microsoft.web/sites/slots", "functionapp", "azurerm_windows_function_app_slot"},
		// non-kind-discriminated types are unaffected
		{"microsoft.keyvault/vaults", "", "azurerm_key_vault"},
		{"microsoft.storage/storageaccounts", "StorageV2", "azurerm_storage_account"},
	}
	for _, c := range cases {
		if got := azureTypeToTFTypeKind(c.typ, c.kind); got != c.want {
			t.Errorf("azureTypeToTFTypeKind(%q, %q) = %q, want %q", c.typ, c.kind, got, c.want)
		}
	}
}

// The SQL `master` system database is not user-manageable and must be excluded, not
// reported as an unsupported-type gap.
func TestExcludesMasterSystemDatabase(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Sql/servers/srv/databases/master"
	if r := excludedReason("azurerm_mssql_database", id); r == "" {
		t.Error("master system database should be excluded")
	}
	user := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Sql/servers/srv/databases/appdb"
	if r := excludedReason("azurerm_mssql_database", user); r != "" {
		t.Errorf("user database must NOT be excluded, got %q", r)
	}
}
