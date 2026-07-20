//go:build integration

package integration

import (
	"os"
	"testing"
	"time"
)

// gcpProject returns the throwaway project to run against, or skips the test.
func gcpProject(t *testing.T) string {
	t.Helper()
	p := os.Getenv("TL_IT_GCP_PROJECT")
	if p == "" {
		t.Skip("set TL_IT_GCP_PROJECT to a throwaway project (billing linked; compute + cloudasset APIs enabled; ADC quota project set) to run the GCP integration test")
	}
	return p
}

// TestIntegrationGCP runs the seed -> onboard -> assert-plan-clean -> teardown loop
// against a throwaway GCP project. Cloud Asset Inventory is project-scoped, so the
// onboard sweeps the whole project; the assertions are the project invariant (no
// drift, no failed stacks) plus proof the seed's resource types onboarded.
func TestIntegrationGCP(t *testing.T) {
	requireTools(t, "gcloud", "terraform", "go")
	project := gcpProject(t)

	terraformSeed(t, "seeds/gcp", map[string]string{"project": project})

	// Networking (network/subnetwork/firewall) exercises cross-reference rewiring;
	// the bucket is a standalone global resource.
	wantTypes := []string{
		"google_compute_network",
		"google_compute_subnetwork",
		"google_compute_firewall",
		"google_storage_bucket",
	}

	deadline := time.Now().Add(15 * time.Minute)
	rep, run := onboardUntil(t, "gcp", project, nil, deadline, wantTypes)

	rep.assertClean(t)
	assertOnboarded(t, run, wantTypes...)
}
