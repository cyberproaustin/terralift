package azure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/model"
)

func TestExcludedReason(t *testing.T) {
	// Security-critical: secret material + storage data content must be excluded.
	for _, tp := range []string{
		"azurerm_key_vault_secret", "azurerm_key_vault_key", "azurerm_key_vault_certificate",
		"azurerm_storage_blob", "azurerm_storage_container", "azurerm_storage_queue",
		"azurerm_storage_table", "azurerm_storage_share",
		"azurerm_automation_module", "azurerm_log_analytics_workspace_table_custom_log",
	} {
		if excludedReason(tp, "") == "" {
			t.Errorf("%s should be excluded (data-plane/built-in)", tp)
		}
	}
	// Control-plane resources must NOT be excluded.
	for _, tp := range []string{
		"azurerm_storage_account", "azurerm_key_vault", "azurerm_resource_group",
		"azurerm_virtual_network", "azurerm_linux_web_app",
	} {
		if excludedReason(tp, "") != "" {
			t.Errorf("%s should NOT be excluded", tp)
		}
	}
	// $Default consumer group excluded by name; a user consumer group kept.
	dflt := "/subscriptions/s/resourceGroups/r/providers/Microsoft.EventHub/namespaces/n/eventhubs/h/consumergroups/$Default"
	if excludedReason("azurerm_eventhub_consumer_group", dflt) == "" {
		t.Error("$Default consumer group should be excluded")
	}
	user := "/subscriptions/s/resourceGroups/r/providers/Microsoft.EventHub/namespaces/n/eventhubs/h/consumergroups/my-cg"
	if excludedReason("azurerm_eventhub_consumer_group", user) != "" {
		t.Error("user consumer group should NOT be excluded")
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

func TestWriteImportBlocksFromState(t *testing.T) {
	dir := t.TempDir()
	// A resource "id" with a template marker (must be escaped); a data resource (must
	// be skipped — never leak data-plane values); a managed resource whose HCL wasn't
	// authored (must be filtered by `generated`); an empty-id instance (skip); and an
	// index_key resource (must render the indexed address).
	state := `{
	  "resources": [
	    {"mode":"managed","type":"azurerm_virtual_network","name":"vnet",
	     "instances":[{"attributes":{"id":"/subs/vnet-${x}","primary_access_key":"SECRET"}}]},
	    {"mode":"data","type":"azurerm_client_config","name":"cur",
	     "instances":[{"attributes":{"id":"data-plane-should-not-appear"}}]},
	    {"mode":"managed","type":"azurerm_orphan","name":"gone",
	     "instances":[{"attributes":{"id":"/subs/orphan"}}]},
	    {"mode":"managed","type":"azurerm_noid","name":"noid",
	     "instances":[{"attributes":{}}]},
	    {"mode":"managed","type":"azurerm_subnet","name":"snet",
	     "instances":[{"index_key":"app","attributes":{"id":"/subs/snet-app"}}]}
	  ]
	}`
	if err := os.WriteFile(filepath.Join(dir, "terraform.tfstate"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	generated := map[string]bool{
		"azurerm_virtual_network.vnet": true,
		"azurerm_noid.noid":            true,
		"azurerm_subnet.snet":          true,
		// azurerm_orphan.gone intentionally absent -> must be filtered
	}
	n, err := writeImportBlocksFromState(dir, generated)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 { // vnet + snet (data skipped, orphan filtered, noid empty-id skipped)
		t.Fatalf("count = %d, want 2", n)
	}
	out, err := os.ReadFile(filepath.Join(dir, "import.tf"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "to = azurerm_virtual_network.vnet") {
		t.Errorf("vnet import block missing:\n%s", s)
	}
	if !strings.Contains(s, `to = azurerm_subnet.snet["app"]`) {
		t.Errorf("indexed address not rendered:\n%s", s)
	}
	if !strings.Contains(s, `$${x}`) { // template marker escaped
		t.Errorf("template marker not escaped:\n%s", s)
	}
	if strings.Contains(s, "SECRET") || strings.Contains(s, "primary_access_key") {
		t.Errorf("non-id attribute leaked into import.tf:\n%s", s)
	}
	if strings.Contains(s, "data-plane-should-not-appear") {
		t.Errorf("data resource id leaked:\n%s", s)
	}
	if strings.Contains(s, "azurerm_orphan") {
		t.Errorf("address absent from generated HCL was not filtered:\n%s", s)
	}
}

func TestWriteImportBlocksFromStateNoState(t *testing.T) {
	// Missing state is benign (hcl-only / empty RG) — no error, no file.
	dir := t.TempDir()
	n, err := writeImportBlocksFromState(dir, nil)
	if err != nil || n != 0 {
		t.Errorf("no-state: n=%d err=%v, want 0/nil", n, err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "import.tf")); statErr == nil {
		t.Error("import.tf written when there was no state")
	}
}
