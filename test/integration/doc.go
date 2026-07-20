// Package integration holds live, cloud-backed integration tests that codify the
// manual validation loop: stand up a seed, onboard it with TerraLift, assert the
// plan round-trip is clean, and tear the seed down.
//
// These tests are guarded by the `integration` build tag, so they never run in a
// normal `go test ./...` or in CI (they need real cloud credentials and create
// billable resources). Run them explicitly:
//
//	go test -tags=integration -v -timeout 40m ./test/integration/...
//
// Each cloud's test skips itself unless its tooling is present and its scope is
// provided by an environment variable, so you can run one cloud at a time:
//
//	AWS   — always runs when the `aws` CLI is authenticated; onboards the whole
//	        account (Resource Explorer is account-scoped).
//	GCP   — set TL_IT_GCP_PROJECT to a throwaway project (billing linked; compute
//	        and cloudasset APIs enabled). gcloud's active project (gcloud config set
//	        project) must be a LIVE project, since Cloud Asset Inventory bills the
//	        search to it — a deleted active/quota project fails with
//	        USER_PROJECT_DENIED. Onboards the whole project (CAI is project-scoped).
//	AZURE — set TL_IT_AZURE_SUBSCRIPTION to a subscription id. The test creates and
//	        destroys a dedicated tl-it-* resource group and onboards ONLY that RG
//	        (--resource-groups), so nothing else in the subscription is touched.
//
// Every test asserts the scope invariant (remainder=0, failedStacks=0, planClean>0)
// plus that the seed's resource types were onboarded, then tears the seed down.
// Because each cloud's enumeration source is eventually consistent for new
// resources, the onboard is retried until the seed is fully indexed. Terraform
// provider downloads are large, so run where the temp dir has room (set TMPDIR to a
// roomy disk if /tmp is small).
package integration
