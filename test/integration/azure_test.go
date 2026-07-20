//go:build integration

package integration

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// azureSubscription returns the subscription to run against, or skips the test.
func azureSubscription(t *testing.T) string {
	t.Helper()
	s := os.Getenv("TL_IT_AZURE_SUBSCRIPTION")
	if s == "" {
		t.Skip("set TL_IT_AZURE_SUBSCRIPTION to a subscription id (a dedicated tl-it-* resource group is created and destroyed) to run the Azure integration test")
	}
	return s
}

// TestIntegrationAzure runs the seed -> onboard -> assert-plan-clean -> teardown
// loop against a dedicated, freshly-created resource group in the given
// subscription. Unlike AWS/GCP, Azure enumeration is scoped down to just that RG
// (--resource-groups), so the assertions concern only the seed's own resources and
// nothing else in the subscription is read or touched.
func TestIntegrationAzure(t *testing.T) {
	requireTools(t, "az", "aztfexport", "terraform", "go")
	sub := azureSubscription(t)

	// Derive collision-resistant names from the clock (no external randomness).
	suffix := fmt.Sprintf("%d", time.Now().Unix()%100000)
	rg := "tl-it-" + suffix    // resource group
	sa := "tlit" + suffix      // storage account: <=24 chars, lowercase alphanumeric
	const location = "eastus2" // matches the seed default

	terraformSeed(t, "seeds/azure", map[string]string{
		"subscription_id": sub,
		"rg_name":         rg,
		"sa_name":         sa,
		"location":        location,
	})

	wantTypes := []string{
		"azurerm_virtual_network",
		"azurerm_network_security_group",
		"azurerm_public_ip",
		"azurerm_storage_account",
	}

	deadline := time.Now().Add(15 * time.Minute)
	rep, run := onboardUntil(t, "azure", sub, []string{"--resource-groups", rg}, deadline, wantTypes)

	rep.assertClean(t)
	assertOnboarded(t, run, wantTypes...)
}
