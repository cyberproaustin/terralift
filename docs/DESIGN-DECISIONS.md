# TerraLift Design Decisions (ADR)

Load-bearing decisions that are easy to "helpfully" undo in a later review. Read
this before changing the redaction / secret-handling behavior.

---

## ADR-001: Ship application configuration; FLAG secrets, do NOT wipe them

**Status:** Accepted (2026-07-17). Decided by the product owner, explicitly.

### Decision
TerraLift **ships application configuration in full** into the generated Terraform.
This covers Azure `app_settings` and `connection_string` blocks, AWS Lambda and ECS
`container_definitions` environment variables, GCP Cloud Run and Cloud Functions
env vars, and similar config maps, **including any secret-looking values that
live in them**. It does **not** blank or strip those config maps.

Secret-looking entries are **reported, not removed**:
- `reports/secrets-review.md` flags every shipped config value that looks like a
  secret (by key name, value pattern, or entropy), across plain lines, JSON blobs
  (e.g. ECS `container_definitions`), heredocs, and split `name`/`value` blocks.
- The operator does the manual hardening (move to Key Vault / Secrets Manager /
  Secret Manager and reference), which is a human judgement call TerraLift will not
  make for them.

### Why (do not revert this)
The entire value proposition is: "point me at my existing cloud, get a Terraform
repo I can stand a pipeline on and switch to IaC." **App config is the
highest-value part of that output.** If TerraLift wipes `app_settings` / env vars,
the generated repo, when applied, brings apps up with missing configuration and
**breaks them**, which defeats the tool. A security reviewer looking only at
"is there a plaintext secret in the repo?" will be tempted to make the redactor
blank these maps. That is the wrong trade: it trades a working migration for a
false sense of cleanliness, and the secret was already in the live environment
regardless.

A previous iteration *did* blank these maps. The owner reversed it:
> "wiping configs is going to cause more harm than good in my opinion. that's one
> of the highest value things we have."

### What IS still redacted (the narrow exception)
Only **unambiguous single secret values**, never config maps, are scrubbed by
`internal/hcl/redact.go`, because a value in one of these positions is *only* ever
a secret and shipping it has no config value:
- exact-name attributes at a resource's TOP LEVEL only: `password`,
  `secret_string`, `*_access_key`, `private_key`, `auth_token`, connection-string
  attributes, admin passwords, etc. (depth-1 only, so an env-var *key* named
  `CLIENT_SECRET` inside a config map is NOT deleted, it ships and is flagged);
- scoped rules: AWS `SecureString` SSM param values, `aws_api_gateway_api_key.value`,
  Azure admin passwords (blanked + `ignore_changes`).

Every such scrub is recorded in `reports/redactions.md` so an operator is never
blindsided by a value that silently vanished. They know exactly what to supply
before cutover.

### If you are a future reviewer
Shipping app config is **intended**. A plaintext secret inside `app_settings` /
env vars is **not a bug to fix by wiping the map**. Verify it is flagged in
`reports/secrets-review.md`; if it isn't, fix the *flagger*
(`internal/reconcile/secrets_review.go`), not by blanking config. Only escalate to
redaction for a *new* class of unambiguous single-secret attribute, via a scoped
rule, and add it to `reports/redactions.md`.
