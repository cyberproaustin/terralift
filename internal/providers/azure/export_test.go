package azure

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cyberproaustin/terralift/internal/model"
)

func TestExcludedReason(t *testing.T) {
	// Security-critical: secret material + storage data content must be excluded.
	for _, tp := range []string{
		"azurerm_key_vault_secret", "azurerm_key_vault_key", "azurerm_key_vault_certificate",
		"azurerm_storage_blob", "azurerm_storage_container", "azurerm_storage_queue",
		"azurerm_storage_table", "azurerm_storage_share",
	} {
		if excludedReason(tp) == "" {
			t.Errorf("%s should be excluded (data-plane)", tp)
		}
	}
	// Control-plane resources must NOT be excluded.
	for _, tp := range []string{
		"azurerm_storage_account", "azurerm_key_vault", "azurerm_resource_group",
		"azurerm_virtual_network", "azurerm_linux_web_app",
	} {
		if excludedReason(tp) != "" {
			t.Errorf("%s should NOT be excluded", tp)
		}
	}
}

func TestBornName(t *testing.T) {
	cases := map[string]string{
		"/subscriptions/s/resourceGroups/rg-bva-mgmt-eus2-dev":                                 "rg_bva_mgmt_eus2_dev",
		"/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/acct1": "acct1",
		"/subscriptions/s/resourceGroups/rg/.../containers/tfstate":                            "tfstate",
		"9lives": "r_9lives", // leading digit -> prefixed
	}
	for in, want := range cases {
		if got := bornName(in); got != want {
			t.Errorf("bornName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSortedContainers(t *testing.T) {
	inv := &model.Inventory{Resources: map[string]*model.Resource{
		"a": {Container: "rg-b"},
		"b": {Container: "rg-a"},
		"c": {Container: "rg-b"}, // dup
		"d": {Container: ""},     // ignored
	}}
	got := sortedContainers(inv)
	if len(got) != 2 || got[0] != "rg-a" || got[1] != "rg-b" {
		t.Errorf("sortedContainers = %v, want [rg-a rg-b]", got)
	}
}

func TestScanResourceAddrs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
resource "azurerm_resource_group" "rg1" {
  name = "x"
}
resource "azurerm_storage_account" "acct" {}
`), 0o644)
	os.WriteFile(filepath.Join(dir, "provider.tf"), []byte(`provider "azurerm" { features {} }`), 0o644)
	got := scanResourceAddrs(dir)
	if !got["azurerm_resource_group.rg1"] || !got["azurerm_storage_account.acct"] {
		t.Errorf("missing expected addresses: %v", got)
	}
	if len(got) != 2 {
		t.Errorf("scanResourceAddrs found %d, want 2: %v", len(got), got)
	}
}
