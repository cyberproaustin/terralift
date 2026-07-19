# TerraLift

TerraLift brings infrastructure you already run in the cloud under Terraform management. You point it at an AWS account, a GCP project, or an Azure subscription, and it produces a clean Terraform repository that adopts those resources as they exist right now. Running `terraform plan` on the output is a clean import: zero resources to add, zero to change, zero to destroy.

It is built for brownfield environments. That means infrastructure created by hand, by ClickOps in a console, by a one-off script, or by a different tool, which now needs to live in version-controlled Terraform without a risky rebuild.

```
                      \   |   /
                     '  .-~-.  '
                   ── ( ███ ) ──
                     .  '-~-'  .
   . ,;. ,.           /   |   \          \|/ \|/ \|/
   ; .,.'; .,     ══════════════▶    \|/ \|/ \|/
   ▒▒▒▒▒▒▒▒▒▒                        ▓▓▓▓▓▓▓▓▓▓▓
   ▒▒▒▒▒▒▒▒▒▒                        ▓▓▓▓▓▓▓▓▓▓▓
             T  E  R  R  A  L  I  F  T
```

## What it does

- Reads the live control plane of a cloud scope, meaning what resources exist and how they are configured.
- Writes a Terraform repository with correct resource names and `import` blocks, so `terraform plan` adopts the resources instead of trying to recreate them.
- Rewires literal cloud IDs into Terraform references, so the repository has real dependencies and applies in the right order.
- Verifies the output with a `terraform plan` round-trip and reports anything that does not import cleanly.
- Flags secrets that appear in shipped configuration, and reports resources that are publicly reachable or hold broad privileges.

TerraLift stays on the control plane by design. It captures configuration. It does not read data-plane content such as secret values, database rows, or object contents. See [How It Works](docs/how-it-works.md) for why that boundary matters.

## Install

You need Go 1.24 or newer.

```
go install github.com/cyberproaustin/terralift/cmd/terralift@latest
```

That drops a `terralift` binary into `$(go env GOPATH)/bin`. Make sure that directory is on your `PATH`. `@latest` installs the newest release. To pin a specific version, use its tag:

```
go install github.com/cyberproaustin/terralift/cmd/terralift@v1.0.0
```

Either way, `terralift version` reports the version you installed.

To build from a clone instead:

```
git clone https://github.com/cyberproaustin/terralift.git
cd terralift
go build -o /usr/local/bin/terralift ./cmd/terralift
```

Confirm the install:

```
terralift version
terralift banner
```

## Quick start

Each cloud needs its own CLI installed and authenticated, and every run needs Terraform on your `PATH`. The exact setup per cloud is in [Getting Started](docs/getting-started.md). Once you are authenticated, one command does the whole run:

```
# GCP: scope is the project ID
terralift onboard --cloud gcp --scope my-project-id

# AWS: scope is the account ID
terralift onboard --cloud aws --scope 123456789012

# Azure: scope is the subscription ID
terralift onboard --cloud azure --scope 00000000-0000-0000-0000-000000000000
```

TerraLift writes everything under `artifacts/<run-id>/`. The Terraform repository is in `repo/` and the reports are in `reports/`.

## What you get

A run produces a directory shaped like this:

```
artifacts/<run-id>/
  repo/
    live/<container>/       one stack per project, account region, or resource group
      generated.tf          resource configuration read from your live infrastructure
                            (Azure names this main.tf)
      import.tf             import blocks that adopt the existing resources
      iam.tf                access control as code (roleassignments.tf on Azure)
      providers.tf          provider source and version pins
      backend.tf            remote state config, keyless and OIDC ready
    README.md               how to adopt this repository
    ci-pipeline.yml         a plan-on-PR and gated-apply starter for your CI
  reports/
    coverage.md             what was onboarded, excluded, or missed
    correctness.md          the plan round-trip result
    secrets-review.md       config values that look like secrets
    redactions.md           secret values that were removed
    hygiene.md              public exposure and over-privileged access
  package/onboarding.zip    repo and reports, zipped for handoff
```

To adopt the resources, initialize and apply the stack:

```
cd artifacts/<run-id>/repo/live/<container>
terraform init
terraform plan     # a clean import: 0 to add, 0 to change, 0 to destroy
terraform apply    # brings the resources under management
```

After the first apply, delete `import.tf`. Import blocks run once.

## How it works, in brief

TerraLift runs six phases. The default runs all of them, and you can run a subset with `--phases`.

| Phase | Name | What it does |
|------:|------------|--------------|
| 1 | Preflight | Checks for the required CLIs and Terraform, validates authentication, resolves the scope |
| 2 | Enumerate | Builds a cloud-neutral inventory of resources, IAM bindings, and public-exposure signals |
| 3 | Export | Authors import blocks with real names and generates the resource HCL |
| 4 | Reconcile | Rewires references, drops over-emitted defaults, lays out the repo, writes the coverage and hygiene reports |
| 5 | Correctness | Runs `terraform plan` and confirms every resource imports with no changes |
| 6 | Package | Assembles the repo, pins providers, adds the backend and CI starter, and zips it |

Phases 1 through 3 are specific to each cloud. Phases 4 through 6 are shared across all clouds. The full explanation is in [How It Works](docs/how-it-works.md).

## Two modes

**Onboard** is the default. It adopts resources in place. The output points at your existing scope and uses import blocks so nothing is recreated.

```
terralift onboard --cloud gcp --scope my-project-id
```

**Clone** produces a portable copy you can stand up in a different scope. Scope-specific values become variables and the import blocks are dropped, so `terraform apply` recreates the infrastructure somewhere new.

```
terralift clone --cloud gcp --scope my-project-id
```

Clone mode is useful for disaster-recovery templates, spinning up a matching staging environment, or moving a workload between projects, subscriptions, or accounts.

## Supported clouds

| Cloud | Enumeration source | Export engine |
|-------|--------------------|---------------|
| AWS | Resource Explorer | `terraform plan -generate-config-out` |
| GCP | Cloud Asset Inventory | `terraform plan -generate-config-out` |
| Azure | Resource Graph | aztfexport |

## Documentation

- [Getting Started](docs/getting-started.md): prerequisites, authentication for each cloud, your first run, and how to apply the output.
- [Commands](docs/commands.md): every subcommand and flag, with examples.
- [How It Works](docs/how-it-works.md): the pipeline in depth, the control-plane boundary, secrets handling, the correctness checks, and known per-cloud limits.
- [Design Decisions](docs/DESIGN-DECISIONS.md): the reasoning behind the choices that shape the output.

## Author

TerraLift is built and maintained by Austin ([cyberproaustin](https://www.linkedin.com/in/cyberproaustin/)). Reach out with questions, ideas, or bug reports:

- Email: [cyberproaustin@gmail.com](mailto:cyberproaustin@gmail.com)
- LinkedIn: [linkedin.com/in/cyberproaustin](https://www.linkedin.com/in/cyberproaustin/)

## License

A license has not been set for this repository yet. Add a `LICENSE` file before publishing or distributing binaries.
