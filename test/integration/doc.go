// Package integration holds live, cloud-backed integration tests that codify the
// manual validation loop: stand up a seed, onboard it with TerraLift, assert the
// plan round-trip is clean, and tear the seed down.
//
// These tests are guarded by the `integration` build tag, so they never run in a
// normal `go test ./...` or in CI (they need real cloud credentials and create
// billable resources). Run them explicitly:
//
//	go test -tags=integration -v -timeout 30m ./test/integration/...
//
// Each cloud's test skips itself unless its CLI is authenticated and Terraform is
// on PATH. The AWS test onboards the whole authenticated account (Resource Explorer
// is account-scoped) and asserts the invariants remainder=0 and failed=0 plus that
// the seed's resource types were onboarded. Terraform provider downloads are large,
// so run where the temp dir has room (set TMPDIR to a roomy disk if /tmp is small).
package integration
