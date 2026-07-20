//go:build integration

package integration

import (
	"fmt"
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
	// the bucket is a standalone global resource. Each is keyed by its unique tl-it-*
	// name so a pre-existing project resource of the same type cannot mask a miss.
	wantSeed := map[string]string{
		"google_compute_network":    `"tl-it-net"`,
		"google_compute_subnetwork": `"tl-it-subnet"`,
		"google_compute_firewall":   `"tl-it-fw"`,
		"google_storage_bucket":     fmt.Sprintf(`"tl-it-%s"`, project),
	}

	deadline := time.Now().Add(15 * time.Minute)
	rep, run := onboardUntil(t, "gcp", project, nil, deadline, wantSeed)

	rep.assertClean(t)
	assertSeedOnboarded(t, run, wantSeed)
}
