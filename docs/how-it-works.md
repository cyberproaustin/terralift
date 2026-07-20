# How It Works

This page explains what TerraLift does under the hood: the pipeline, the control-plane boundary, how it handles secrets, how it checks its own output, and the known limits for each cloud. Read it if you want to understand the output rather than just produce it.

## The pipeline

TerraLift runs six phases. Phases 1 through 3 are specific to each cloud. Phases 4 through 6 are shared code that runs the same way regardless of cloud.

### Phase 1: Preflight

Confirms the required CLIs and Terraform are installed, validates that you are authenticated, and resolves the scope you passed into something the rest of the run can use. If a tool is missing or you are not logged in, this is where you find out, before any work starts.

### Phase 2: Enumerate

Builds a cloud-neutral inventory of what is running. Each cloud has a different source:

- AWS reads from Resource Explorer.
- GCP reads from Cloud Asset Inventory, which returns metadata and full resource configuration in a single sweep.
- Azure reads from Resource Graph.
- GitHub reads from the GitHub API via the `gh` CLI.

Enumeration also collects IAM bindings and public-exposure signals, such as a firewall open to the internet or an object made public — for the clouds that have such planes. A flat provider like GitHub declares it has no IAM or exposure plane, so those reports read "not applicable" rather than "checked, found nothing." The result is written to `inventory.json`, so later phases can reload it.

Each enumerated resource is classified into a Terraform type. Most map one to one, but a cloud's inventory sometimes reports several distinct resources under a single type. TerraLift disambiguates these from the resource's own attributes: a GCP load-balancer component is resolved to its regional or global Terraform type by location, and an AWS `rds:cluster` is resolved to Aurora, DocumentDB, or Neptune by its engine.

Some services are not indexed by the cloud's inventory at all. AWS Resource Explorer, for example, does not list SecurityHub, Organizations, or Identity Center resources. For those, a supplemental enumeration step queries the service's own APIs directly and injects the results into the inventory, so they are onboarded alongside everything else.

### Phase 3: Export

Turns the inventory into born-correct Terraform. Two things happen here.

First, TerraLift authors an `import` block for each resource. It picks a stable, readable address (for example `google_compute_network.prod_vpc`, not a random hash) and derives the correct import ID for that resource type from the cloud's own identifiers. Born-correct means the names and IDs are right the first time, so you do not have to rename resources or fix import IDs by hand.

Second, it generates the resource HCL. On AWS, GCP, and GitHub this uses `terraform plan -generate-config-out`, then curates the result to drop provider defaults that the generator over-emits (and to author back attributes the generator wrongly drops — a repository's download setting, or a webhook's URL that it marks sensitive). On Azure it uses `aztfexport`.

### Phase 4: Reconcile

Takes the raw export and makes it a clean, correct repository.

- **Reference rewiring.** Cloud exports contain literal IDs, such as a subnet ID pasted into a virtual machine. TerraLift rewrites those literals into Terraform references. A literal ID becomes `.id`, a self-link becomes `.self_link`, a service-account email becomes `.email`, a static IP becomes `.address`. This gives the repository real dependencies, so it applies in the right order and stays correct if IDs change on a rebuild.
- **Filtering.** It drops resources that should not be managed as standalone Terraform, such as auto-created default networking, service-managed children, and provider defaults that create noise or conflicts.
- **Layout.** It arranges the stacks under `repo/live/<container>/`, one per project, account region, or resource group.
- **Reports.** It writes the coverage, secrets, and hygiene reports described below.

### Phase 5: Correctness

Runs `terraform plan` against the generated stacks and checks the result. A resource that imports with no changes is captured correctly. Anything that shows a create, a destroy, or an unexpected change is flagged in `correctness.md`. This is the round-trip oracle: it proves the output does what it claims.

### Phase 6: Package

Assembles the final repository, pins the provider source and version, adds a keyless remote-state backend and a CI starter, and zips the repo and reports into `package/onboarding.zip` for handoff.

## Two oracles

TerraLift answers two separate questions, and keeps them separate so the reporting stays honest.

- **Coverage.** Was each resource captured at all? The coverage report splits every enumerated resource into onboarded, intentionally excluded, or a gap. A gap is a type TerraLift could not map. A gap count of zero means nothing manageable was missed.
- **Correctness.** Was each captured resource captured correctly? The plan round-trip in phase 5 answers this. A high coverage number over an output that does not plan cleanly would be misleading, so the two are measured independently.

## Control plane only

TerraLift captures the control plane. That is the configuration of your resources: the shape of a network, the settings on a database, the policy on a bucket. It does not read the data plane: the values inside secrets, the rows in a database, the objects in a bucket.

This is a deliberate boundary. Reading the data plane would require broad, sensitive permissions, would pull secret material into files on disk, and is not what Terraform manages anyway. Terraform manages the resource, not its contents. Keeping to the control plane means TerraLift runs with read-only configuration access and never has your secret values in hand.

## How secrets are handled

Application configuration and secrets need different treatment, and TerraLift treats them differently.

**Application configuration ships.** Environment variables, app settings, and connection settings are the highest-value part of moving to Terraform, and wiping them would break the applications. So TerraLift preserves them in the generated HCL exactly as they run.

**Look-alike secrets are flagged, not removed.** Some of that shipped configuration will contain values that look like secrets, such as a password inside a database connection string or an API key in an environment variable. TerraLift lists each one in `secrets-review.md` with its location, so you can move the real secret into a managed store (a key vault, secrets manager, or secret manager) and replace the literal with a reference. This is judgment work that a human should do, so the tool flags rather than decides.

**Unambiguous single secrets are redacted.** Values that are unmistakably a single secret, such as a private key, a standalone password field, or an access key, are removed from the output before it is written. These are recorded in `redactions.md` by attribute name, not value. They are not in the repository, so you supply them before a from-scratch apply.

The generated repository also ships a `.gitignore` that excludes Terraform state, because state can contain data-plane secrets. TerraLift never ships a state file.

## Per-cloud details

The high-level flow is the same everywhere, but the adoption mechanism differs.

### AWS and GCP

Both use `terraform plan -generate-config-out` for the resource HCL and ship import blocks in `import.tf`. Adoption is a plain `terraform plan` on the repository. IAM is authored as code.

Some attributes are write-only, meaning the cloud accepts them at create time but never returns them on read. Examples are a KMS key's lockout-bypass flag or a VPC peering auto-accept flag. TerraLift pairs those with a `lifecycle { ignore_changes }` block, so adoption stays plan-clean while the attribute still drives a from-scratch rebuild.

### Azure

Azure uses `aztfexport`, which adopts resources by importing them into a Terraform state file rather than by emitting import blocks. That state file can contain data-plane secrets, so TerraLift does not ship it. Instead, TerraLift reads the addresses and resource IDs out of that state and generates its own `import.tf`, so the shipped repository adopts the resources the same way the AWS and GCP output does, without ever carrying the state. Azure RBAC is authored to `roleassignments.tf` with its own import blocks.

### GitHub

GitHub is a flat provider: the scope is a single organization or user login, and everything lands in one stack — there are no regions, resource groups, or projects to lay out. The scope resolves to whichever the login is, so the same command adopts either an org or a personal account. Authentication uses the token the `gh` CLI is already signed in with, published to the environment so Terraform authenticates without a token ever being written into the generated config.

Coverage is repositories (with their webhooks and branch protection), organization membership, teams and team membership, and organization webhooks. Two GitHub-specific rules keep the output honest. First, GitHub auto-creates nine default issue labels in every repository; those are skipped, so only labels you created are adopted. Second, an Actions secret's value is write-only — the API never returns it — so a secret cannot be adopted plan-clean, and adopting one with a placeholder value would overwrite the real secret on the first apply. TerraLift surfaces secrets so you know they exist but leaves them unadopted, to be managed out-of-band with the value supplied at apply time. Enumerating org webhooks and teams needs the `admin:org_hook` and `admin:org` token scopes respectively; without them, those resources are skipped with a note rather than failing the run.

Clone mode (`terralift clone`) produces a portable copy. Scope-specific attributes such as the project, region, or resource group become variables, resource names get an optional prefix and suffix so globally unique names do not collide, and the import blocks are dropped so `terraform apply` creates the resources in a new scope.

Some things cannot be reproduced identically across scopes, and these are platform facts, not tool gaps:

- **Cloud-provisioned artifacts.** When a cloud service creates resources on your behalf, those cannot be cloned as-is. A GCP second-generation Cloud Function, for example, stores its source archive in a bucket the Functions service creates, and that source does not exist in the target scope.
- **Service-agent grants.** A grant to a cloud's own service identity is specific to a project, subscription, or account. A cross-scope customer-managed-encryption-key grant to a storage service agent, for example, has to be set up in the target with a data-source pattern.
- **Owned resources.** A public DNS zone requires you to own the domain. A clone in a scope that does not own it will not create it.
- **Redacted secrets.** Values removed as single secrets, listed in `redactions.md`, must be supplied before apply.

Beyond these, some resources may fail to create in a given scope for reasons that belong to your cloud environment rather than to TerraLift, such as a service quota that is set to zero, a VM size with no regional capacity, or a service that is disabled on the subscription. Those show up as apply-time errors from the provider and are resolved in your cloud account, not in the tool.

## Design decisions

The reasoning behind the choices that shape the output, including why application configuration ships intact and secrets are flagged rather than wiped, is documented in [Design Decisions](DESIGN-DECISIONS.md).
