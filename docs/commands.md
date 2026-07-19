# Commands

TerraLift is a single binary with a small set of subcommands:

```
terralift <command> [flags]
```

Run `terralift --help` for the top-level list, or `terralift <command> --help` for a command's flags. This page documents each command and every flag.

## Commands at a glance

| Command | Purpose |
|---------|---------|
| `onboard` | Adopt live infrastructure into a plan-clean Terraform repository |
| `clone` | Generate a re-targetable copy you can stand up in a new scope |
| `version` | Print the TerraLift version |
| `banner` | Print the startup banner |

## terralift onboard

Enumerates a scope and generates a Terraform repository that adopts the live resources through import blocks. Running `terraform plan` on the output is a clean import with no creates and no destroys.

```
terralift onboard --cloud gcp --scope my-project-id
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--cloud` | (required) | Cloud provider: `aws`, `azure`, or `gcp`. |
| `--scope` | (required) | The scope ID. Project ID for GCP, account ID for AWS, subscription ID for Azure. |
| `--scope-type` | per cloud | Scope type: `project`, `folder`, `organization`, `subscription`, or `account`. Defaults to the natural top level for each cloud: `project` for GCP, `account` for AWS, `subscription` for Azure. |
| `--resource-groups` | all | Restrict the run to a comma-separated list of containers or resource groups. Empty means the whole scope. |
| `--artifacts` | `artifacts` | Output root. TerraLift writes each run under `<artifacts>/<run-id>/`. |
| `--phases` | `1,2,3,4,5,6` | Comma-separated phases to run. See [Phases](#phases). |
| `--hcl-only` | false | Generate HCL only, with no state or import round-trip. |
| `--dry-run` | false | Detect and report only. Runs enumeration and the reports, writes no repository. |
| `--no-banner` | false | Suppress the startup banner. |
| `--verbosity` | `info` | Log level: `debug`, `verbose`, `info`, `warn`, or `error`. |

### Examples

```
# Full onboarding of a GCP project
terralift onboard --cloud gcp --scope my-project-id

# AWS account in a specific region (set the region in the environment)
AWS_REGION=us-east-1 terralift onboard --cloud aws --scope 123456789012

# Azure, limited to two resource groups
terralift onboard --cloud azure --scope <sub-id> --resource-groups rg-app,rg-data

# Detection and reports only, no repository written
terralift onboard --cloud gcp --scope my-project-id --dry-run

# Write output somewhere specific and turn up logging
terralift onboard --cloud gcp --scope my-project-id --artifacts ./out --verbosity debug
```

## terralift clone

Generates a portable copy of the infrastructure that you can stand up in a different scope. Scope-specific values such as the project, region, or resource group become variables, and the import blocks are dropped so `terraform apply` recreates the resources rather than adopting existing ones. This is migration mode, and it implies `--hcl-only`.

```
terralift clone --cloud gcp --scope my-project-id
```

It accepts the same flags as `onboard`. To stand the clone up in a new scope, set the generated variables in a `terraform.tfvars` file and run `terraform apply` against the target.

Clone mode fits three common jobs:

- A disaster-recovery template that reproduces an environment in a new region or account.
- A staging or test environment that matches production.
- Moving a workload between projects, subscriptions, or accounts.

Some resources cannot be reproduced identically across scopes. Cloud services provision certain resources on your behalf (for example, a function's backing build artifacts, or a default service identity), and secrets that were redacted from the output must be supplied before apply. See the per-cloud limits in [How It Works](how-it-works.md).

## terralift version

Prints the version string.

```
terralift version
# terralift v1.0.0
```

The same value is available with `terralift --version`.

## terralift banner

Prints the startup banner. It is colored on an interactive terminal and falls back to plain text when the output is piped or redirected.

```
terralift banner
```

## Phases

TerraLift runs as six phases. The default runs all of them in order. You can run a subset with `--phases`, which is useful for debugging or for re-running a late phase against an existing run.

| Phase | Name | What it does |
|------:|------------|--------------|
| 1 | Preflight | Checks for the required CLIs and Terraform, validates authentication, resolves the scope. |
| 2 | Enumerate | Builds a cloud-neutral inventory of resources, IAM bindings, and public-exposure signals, and writes `inventory.json`. |
| 3 | Export | Authors import blocks with real resource names and generates the resource HCL. |
| 4 | Reconcile | Rewires references, drops over-emitted provider defaults, lays out the repository, and writes the coverage, secrets, and hygiene reports. |
| 5 | Correctness | Runs `terraform plan` and confirms every resource imports with no changes. |
| 6 | Package | Assembles the final repository, pins providers, adds the backend and CI starter, and zips it. |

Phases 1 through 3 are specific to each cloud. Phases 4 through 6 are shared.

Two notes on running subsets:

- Phase 2 writes `inventory.json`, and phase 3 can reload it, so you can run enumeration once and re-run export against it.
- Phases 4 through 6 need the phase 2 and 3 output in the same process, so run `4,5,6` together with the phases that produced their input, or run the full pipeline.

Examples:

```
# Enumerate and report only, then stop
terralift onboard --cloud gcp --scope my-project-id --phases 1,2

# Skip the plan round-trip (phase 5) and the package step is still available
terralift onboard --cloud gcp --scope my-project-id --phases 1,2,3,4,6
```

## Output and exit behavior

Each run writes to `<artifacts>/<run-id>/`. The layout is documented in the [README](../README.md) and expanded in [How It Works](how-it-works.md).

TerraLift exits non-zero on a fatal error, such as a failed enumeration or a missing required flag, and prints a single clear error line. A resource that cannot be mapped is reported as a coverage gap, not a fatal error, so a run completes and tells you honestly what it could and could not capture.
