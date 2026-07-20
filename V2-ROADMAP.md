# TerraLift v2 Roadmap: Breadth With Quality

This document is the plan for expanding TerraLift from three clouds to broad
multi-provider coverage while keeping the thing that makes it worth using:
plan-clean, curated, verified output. It is grounded in a code-level analysis of
Terraformer (the closest competitor, 44 providers) and of TerraLift's own
extension cost.

Status: planning input for v2. v1 stays on bug fixes across AWS, GCP, and Azure.

---

## 1. The strategic insight that shapes everything

Terraformer supports 44 providers because it does almost no work per provider. It
launches the real `terraform-provider-*` plugin binary over gRPC and calls the
plugin's `ReadResource` / `ImportResourceState` to read each resource. A
contributor writes only an `InitResources()` that lists resource IDs; the
framework does the read, the HCL printing, and the state file. That is the entire
breadth engine.

That same design is why Terraformer's output is not plan-clean. It dumps whatever
the plugin returns: every computed default, `tfer--` machine names, references
only where a maintainer hand-declared them, and a full `terraform.tfstate` with
unscrubbed secrets. Terraformer traded output quality for contributor throughput,
on purpose.

Here is the part that matters for us. **We do not need Terraformer's mechanism to
get its breadth.** The export path we already run for AWS and GCP,
`terraform plan -generate-config-out`, is a Terraform core feature since 1.5 and
is provider-agnostic. It can generate configuration for any resource, from any
Terraform provider, as long as we hand it a valid import block. That means our
quality path already works for Datadog, GitHub, Cloudflare, Okta, and the rest,
not just the big three.

So the v2 goal restated in engineering terms:

> Both tools need "list the resource IDs" per provider. Terraformer stops there
> and ships the raw dump. We go further and curate the generated HCL into
> plan-clean output. The v2 job is to make that extra curation step cheap and
> reusable, and to build the enumeration scaffolding that lets a new provider be
> mostly "list IDs plus a type map plus curation rules."

There is no free lunch. Quality costs the curation work per resource type, and
curation can only be finished by running a live round-trip. Framework work lowers
that per-provider cost. It does not remove it. That single fact is why the
recommendation at the end is framework-first and demand-driven, not a race to 44.

---

## 2. Where we stand versus Terraformer

| Measure | TerraLift (v1) | Terraformer |
|---|---|---|
| Providers | 3 | 44 |
| Service registrations, our three clouds | (subset) | 193 (AWS 90, GCP 68, Azure 35) |
| Service registrations, all providers | n/a | 622 plus dynamic Kubernetes |
| Output quality | Plan-clean, curated, verified, secrets flagged, state never shipped | Raw plugin dump, computed-attribute noise, `tfer--` names, partial references, state shipped with secrets |

Two cautions on the raw counts:

- Terraformer's GCP 68 is inflated. It splits Compute Engine into roughly 50
  fine-grained sub-resources (`addresses`, `disks`, `forwardingRules`, and so on).
  Measured by service family the gap inside GCP is much smaller than 68 suggests.
- A higher service count over output that does not plan clean is not obviously
  better. Our 193-service target inside our own clouds is a real number, but it is
  a coverage target, not a quality target.

The full catalog (all 44 providers, service counts, and categories) is in the
appendix at the end.

---

## 3. What a new provider costs us today

Measured against the three existing providers. Production line counts are actual.

| Provider | Production LOC |
|---|---:|
| AWS | 2,287 |
| GCP | 1,572 |
| Azure | 1,553 |

The `CloudProvider` interface (`internal/provider/provider.go`) has six methods.
Roughly 40 percent of a provider's lines are boilerplate (preflight, CLI
plumbing, templates, the type maps) carrying about 10 percent of the difficulty.
The other 50 percent of the lines carry 90 percent of the skill, concentrated in
four hard parts:

1. **generate-config-out / export-tool curation. The single biggest cost.** About
   870 lines in `aws/export.go` and 330 in `gcp/export.go`. `generate-config-out`
   and `aztfexport` both emit HCL that does not plan clean: over-emitted empty and
   conflicting attributes, dropped write-only resources, unretrievable secrets.
   Making it plan-clean is per-resource-type and can only be finished by running
   it live. This is the number-one reason each provider took real effort.
2. **Import-ID derivation** (about 90 to 145 LOC). Turning a cloud resource ID
   into the exact Terraform import string, which is wildly per-type. Azure gets
   this free because aztfexport derives it.
3. **The reference map (`AddressByID`) consumed by `reconcile.Rewire`** (about 40
   to 90 LOC, embedded in export). Different ID forms must resolve to different
   attributes: a self-link to `.self_link`, a service-account email to `.email`, a
   static IP to `.address`, not always `.id`. Getting this wrong yields a repo
   that imports clean but breaks on rebuild.
4. **IAM authoring** (0 to 147 LOC, a per-cloud fork). There is no shared IAM
   authoring today. GCP writes `iam.tf`, Azure writes `roleassignments.tf`, AWS
   writes none because IAM rides through generate-config-out as ordinary
   resources.

Plus a smaller item, **public-exposure enrichers** (about 40 to 70 LOC per cloud).

### What is already shared, and free to every new provider

Roughly 2,900 lines in `internal/` that a provider never rewrites:

- `model` — the cloud-neutral inventory contract.
- `reconcile` — coverage set-diff oracle, hygiene report, the `Rewire`
  literal-ID-to-reference rewriter, clone migration, secrets review.
- `pipeline` — phases 4 through 6 entirely: the `live/<container>` layout,
  rewire orchestration, gitignore / README / CI emission, `terraform fmt`, the
  plan round-trip correctness oracle, and packaging.
- `hcl` — the structure-aware redaction engine.
- `tf` — the terraform exec wrapper and plan-JSON no-op classifier.
- `naming` — born-correct, de-collided resource labels.
- `core` — run, paths, logging, artifacts.

A new provider does not rewrite coverage math, the round-trip oracle, packaging,
reference rewiring, redaction, or naming. It writes everything that touches the
specific cloud's APIs and its Terraform provider's quirks.

---

## 4. Where the current design assumes a big-3 hyperscaler shape

The interface technically accepts anything, but the model and shared phases assume
a hyperscaler shape. A flat SaaS provider (Datadog, GitHub, Cloudflare, Okta)
collides with four assumptions:

1. **Scope is a closed enum.** `model.ScopeType` is exactly
   `project | folder | organization | subscription | account`. A SaaS tenant is
   none of these. Fix: make `ScopeType` an open string, add `tenant` / `global`,
   and let a provider default it.
2. **The `live/<container>` layout assumes meaningful sub-scopes.** Packaging
   groups by region, resource group, or project. A flat SaaS has no sub-scoping,
   so it produces a degenerate single stack. Fix: support a flat, container-less
   layout.
3. **IAM assumes a cloud RBAC plane with inheritance.** SaaS permissions are
   usually just more resources (`github_team_membership`, `datadog_role`), not a
   separate binding plane with org and folder ancestry. Fix: make IAM authoring
   and the hygiene report opt-in.
4. **Exposure assumes network reachability.** "Publicly exposed" is meaningless
   for a Datadog monitor. Fix: make exposure opt-in per provider.

What already generalizes cleanly: import-ID derivation, `AddressByID` and
`Rewire`, naming, coverage math, the round-trip oracle, packaging, and redaction
are all shape-agnostic and would serve a SaaS provider unchanged.

**The real structural gap for SaaS is enumeration.** Each hyperscaler leans on a
single unified inventory service: AWS Resource Explorer, GCP Cloud Asset
Inventory, Azure Resource Graph. No SaaS has one. For Datadog you call
`list monitors`, `list dashboards`, `list synthetics`, and so on, per type, and
assemble the inventory by hand. The interface accepts this, but there is zero
reusable enumeration scaffolding today, so a SaaS enumerator is high-effort
greenfield. That is the thing to fix before SaaS providers are worth doing.

---

## 5. The v2 work, as a list

Ordered so that framework investment comes before breadth, because the framework
lowers the cost of everything after it.

### Workstream 0: Framework (do this first)

These are the investments that most reduce per-provider cost, in rough ROI order.

- [ ] **Extract a shared generate-config-out driver plus a declarative curation
  toolkit.** The 870 lines in `aws/export.go` and 330 in `gcp/export.go` are the
  same idea reimplemented: regex prune passes, brace-balanced nested-block
  dropping, per-type block editors, and authoring the resources the generator
  drops. Move this into a shared `internal/export` (or `internal/hcl`) with
  per-type curation rules registered declaratively. This is the single biggest
  lever. It is the hard part of every hyperscaler provider.
- [ ] **Extract a shared "reconcile against generated" helper.** The
  scan-generated-addresses, drop-orphan-import-blocks, rewrite-import.tf dance is
  near-verbatim in all three providers, and the address-scanning regex is
  copy-pasted three times.
- [ ] **Build an enumeration kit.** Pagination, native-type to Terraform-type
  classification, and floor-plus-enrich scaffolding, so a new provider fills in
  API calls rather than re-implementing paging and two type maps. This is the
  precondition that makes SaaS providers tractable.
- [ ] **Add a declarative import-ID override registry** shared across providers,
  replacing the per-provider `importid.go`.
- [ ] **De-hyperscaler-ize the model.** Open `ScopeType`, make `Container`
  optional with a flat-layout path in `pipeline`, and make hygiene, exposure, and
  IAM opt-in so a flat provider produces a clean report instead of a
  hyperscaler-shaped one full of zeros.

Items 1 and 2 alone would cut the hardest 30 to 40 percent off a hyperscaler
provider. Item 3 is what makes SaaS providers worth attempting at all.

### Workstream 1: Deepen the big three (highest value, lowest risk)

Before adding new clouds, close the coverage gap inside the clouds we already
support. Same plumbing, same auth, same curation engine. This is where most real
user infrastructure actually lives.

- [ ] Expand the AWS type map and curation toward the 90 services Terraformer
  covers. Prioritize by real-world frequency, not by matching their list.
- [ ] Expand GCP coverage. Note their 68 is Compute-split, so the true family gap
  is smaller. Focus on non-compute services (data, messaging, serverless).
- [ ] Expand Azure coverage toward the 35 services, within the aztfexport path.
- [ ] Grow the per-provider coverage test corpus so each added type is verified by
  a live round-trip, not just written.

### Workstream 2: A second hyperscaler

- [ ] **Oracle Cloud (OCI)** is the natural next cloud. It fits the model almost
  perfectly: compartments map to container and scope, it has IAM policies, network
  exposure, and a Search service that can act like Resource Explorer or Cloud
  Asset Inventory. Confirm whether OCI's Terraform provider supports
  `-generate-config-out`. If it does, reuse the AWS and GCP path. If not, we need
  an aztfexport-equivalent or hand-authoring, which adds materially to cost.

### Workstream 3: Other IaaS clouds (cloud-shaped, moderate cost)

These are hyperscaler-shaped enough that the model mostly fits. Prioritize by user
demand.

- [ ] DigitalOcean (16 services in Terraformer), Linode (9), Vultr (11), and the
  larger IBM (50), IONOS (37), Tencent (28) if demand justifies the type-map cost.

### Workstream 4: Flat SaaS and platform providers (needs Workstream 0 flexes)

Only worthwhile after the model flexes in Workstream 0 land, and each needs a
bespoke enumerator.

- [ ] GitHub, Datadog, Cloudflare, Okta are the highest-profile targets. Caveat:
  our headline features (hygiene and exposure) deliver little value on a SaaS, so
  the pitch for these is codification and drift-baseline, not security hygiene.

---

## 6. Per-provider cost model (use this to size any addition)

At the current quality bar, plan-clean adoption verified by the round-trip oracle:

**A new hyperscaler-shaped cloud (for example OCI):** about 1,500 to 2,300
production LOC plus real tests, roughly 3 to 6 focused engineer-weeks, about 80
percent of it in the type map and curation, not the interface. Live spot-testing
is the real bottleneck.

**A flat SaaS provider (for example Datadog):** about 800 to 1,500 production LOC,
roughly 2 to 4 engineer-weeks, a large share of it in the hand-rolled enumerator,
plus a one-time framework tax to de-hyperscaler-ize the shared layer. Fewer
resource types than a hyperscaler, but no unified enumeration to lean on.

The recurring bottleneck for both is live validation. Each resource type is only
"done" once a live round-trip proves it plans clean. That caps how fast breadth
can grow regardless of how good the framework is.

---

## 7. Recommendation

Matching Terraformer's full 44 providers and 622 services at our quality bar is a
multi-year effort for a small team, because every resource type carries live
validation that Terraformer skips. Do not race to 44. The winning sequence:

1. **Framework first (Workstream 0).** It pays for itself on every provider after.
2. **Deepen the big three (Workstream 1).** Highest value, lowest risk, where real
   infrastructure lives.
3. **Add OCI (Workstream 2).** Proves the framework generalizes to a second
   hyperscaler.
4. **Then pick IaaS and SaaS providers by actual user demand,** not by matching a
   competitor's list.

The honest positioning stays the same: not the broadest tool, but the one whose
output you can trust. Breadth should grow toward that, never at its expense.

---

## Appendix: Terraformer breadth catalog

44 providers, 622 static service registrations plus dynamic Kubernetes. Service
names extracted from each provider's `GetSupportedService()` map.

**Categories:** a Hyperscale, b Other IaaS/PaaS, c Kubernetes, d SaaS/DevOps,
e Networking/CDN/DNS, f Identity, g Observability, h Misc.

| Provider | Services | Category |
|---|---:|---|
| aws | 90 | a |
| gcp | 68 | a |
| ibm | 50 | b |
| vault | 38 | h |
| ionoscloud | 37 | b |
| okta | 37 | f |
| azure | 35 | a |
| datadog | 30 | g |
| tencentcloud | 28 | b |
| auth0 | 16 | f |
| digitalocean | 16 | b |
| alicloud | 11 | b |
| commercetools | 11 | d |
| vultr | 11 | b |
| myrasec | 10 | e |
| octopusdeploy | 10 | d |
| honeycombio | 9 | g |
| linode | 9 | b |
| panos | 9 | e |
| github | 8 | d |
| mackerel | 8 | g |
| rabbitmq | 8 | h |
| newrelic | 7 | g |
| pagerduty | 7 | d |
| heroku | 6 | b |
| azuread | 5 | f |
| cloudflare | 5 | e |
| opal | 5 | f |
| equinixmetal | 4 | b |
| yandex | 4 | b |
| azuredevops | 3 | d |
| fastly | 3 | e |
| launchdarkly | 3 | d |
| ns1 | 3 | e |
| openstack | 3 | b |
| opsgenie | 3 | d |
| gitlab | 2 | d |
| grafana | 2 | g |
| logzio | 2 | g |
| xenorchestra | 2 | b |
| gmailfilter | 2 | h |
| keycloak | 1 | f |
| mikrotik | 1 | e |
| kubernetes | dynamic | c |

**Category totals:** Hyperscale 193 (3 providers), Other IaaS 181 (12),
SaaS/DevOps 47 (8), Identity 64 (5), Observability 58 (6), Networking 31 (6),
Misc 48 (3), Kubernetes dynamic (1).
