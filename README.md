# TerraLift

A multi-cloud tool that brings existing (ClickOps / brownfield) cloud infrastructure
**under Terraform** — enumerate the control plane, export it with born-correct
naming, reconcile it into a clean, module-structured, **plan-clean** repo, and
package it pipeline-ready. Control-plane only by design (no data-plane state:
secret *values*, DB schema, blob contents).

> **Status:** Go rewrite in progress. The original **Azure** implementation
> (PowerShell, feature-complete + tested) lives on the [`legacy/powershell-azure`](../../tree/legacy/powershell-azure)
> branch and is being ported here. **GCP** is the first cloud in the Go version; **AWS** follows.

## Why a rewrite (PowerShell → Go)

The tool is CLI-glue + JSON transformation + templating that will run across many
clouds and be installed by others. Go gives us:
- a **single static binary** (zero runtime dependency — the biggest adoption factor);
- **ecosystem alignment** — import `hashicorp/terraform-exec` + `terraform-json` to
  drive Terraform with typed structs, and reuse cloud/export libraries directly;
- **goroutine concurrency** for org-scale enumeration and export.

The valuable IP is the *design*, not the syntax — it ports directly, and the
legacy branch's unit tests are the port's spec.

## How it works

Six independently-resumable phases. Phases 1–3 are per-cloud; **Phases 4–6 are
cloud-agnostic** and are the bulk of the reusable core.

| Phase | What it does |
|------:|--------------|
| 1 Preflight | tool/dependency check + auto-install, auth, scope |
| 2 Enumerate | build a cloud-neutral inventory (metadata + full config + IAM/policy/exposure enrichers) |
| 3 Export | born-correct Terraform: author `import{}` blocks with real names, generate/curate HCL |
| 4 Reconcile | coverage gap, reference re-wiring, schema-driven filtering, tag/label-driven `modules`+`live` layout, hygiene report |
| 5 Correctness | `terraform plan` round-trip oracle — flag anything that doesn't `no-op` |
| 6 Package | organized repo + pinned providers/backend + OIDC pipeline + reports, zipped |

Two independent oracles: **coverage** (captured at all?) and the **plan round-trip**
(captured correctly?).

## Architecture

```
cmd/terralift/          CLI entrypoint (flags → run → phase pipeline)
internal/
  model/                cloud-neutral types: Inventory, Resource, Scope, IAMBinding, Exposure  ← the interface
  provider/             CloudProvider interface + registry (the per-cloud seam)
  core/                 run context, config, paths, logger, checkpoint, retry
  providers/{gcp,azure,aws}/   per-cloud implementations
  reconcile/            shared Phases 4–6
  tf/                   terraform driver (hashicorp/terraform-exec) + schema
```

Each cloud implements `provider.CloudProvider` (CheckDependencies, Connect,
Enumerate, Export, Templates); everything downstream consumes the cloud-neutral
`model.Inventory`.

## Build

```sh
go build ./...
go build -o terralift ./cmd/terralift
./terralift --cloud gcp --scope-type organization --scope <org-number> --dry-run
```
