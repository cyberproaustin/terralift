# Getting Started

This guide takes you from a fresh install to a Terraform repository that manages your live infrastructure. It covers what to install, how to authenticate for each cloud, how to run your first onboarding, and how to apply the result.

## 1. Install the tools

TerraLift drives the official cloud CLIs and Terraform. It does not bundle them, so they need to be on your `PATH`.

Every run needs:

- **Terraform** 1.5 or newer. TerraLift uses `terraform plan -generate-config-out` and the plan round-trip.
- **TerraLift** itself. See the install steps in the [README](../README.md).

Then, for the cloud you are onboarding:

| Provider | Required tools |
|----------|----------------|
| GCP | `gcloud` (the Google Cloud CLI) |
| AWS | `aws` (the AWS CLI v2) |
| Azure | `az` (the Azure CLI) and `aztfexport` |
| GitHub | `gh` (the GitHub CLI) |

Install `aztfexport` from Microsoft:

```
# macOS
brew install aztfexport

# Go
go install github.com/Azure/aztfexport@latest
```

## 2. Authenticate

TerraLift reads your live infrastructure using your existing credentials. It never asks for or stores keys. Authenticate with the cloud's normal login flow before you run it.

### GCP

TerraLift uses two credential planes: the `gcloud` CLI for enumeration, and Application Default Credentials (ADC) for the Terraform provider.

```
gcloud auth login
gcloud auth application-default login
gcloud config set project my-project-id
```

Enumeration reads from Cloud Asset Inventory. Enable the API once per project:

```
gcloud services enable cloudasset.googleapis.com --project my-project-id
```

Your identity needs read access to the project. `roles/viewer` plus `roles/cloudasset.viewer` is enough for enumeration. The `terraform plan` round-trip in phase 5 imports resources into a local state file, which also needs read access to each resource.

### AWS

Use any standard AWS credential source: an environment profile, `aws configure`, SSO, or instance credentials. Set the region because several services are regional.

```
aws configure          # or export AWS_PROFILE / AWS_ACCESS_KEY_ID, etc.
export AWS_REGION=us-east-1
aws sts get-caller-identity   # confirm you are authenticated
```

Enumeration reads from AWS Resource Explorer. It must be enabled with an aggregator index in the region you are scanning. If it is not set up, enable it once in the AWS console under Resource Explorer, or with the CLI, and give it a few minutes to index.

### Azure

Log in with the Azure CLI and select the subscription you are onboarding.

```
az login
az account set --subscription 00000000-0000-0000-0000-000000000000
export ARM_SUBSCRIPTION_ID=00000000-0000-0000-0000-000000000000
```

Enumeration reads from Azure Resource Graph, which is available by default. The export step uses `aztfexport`, which imports resources into a local state file, so your identity needs Reader on the subscription or the resource groups you target.

### GitHub

Sign in with the GitHub CLI. TerraLift uses the token `gh` holds; the Terraform provider inherits it from the environment, so no token is written into the generated config.

```
gh auth login
gh auth status   # confirm you are authenticated
```

The scope is an organization or a user login (`--scope my-org`; it defaults to your own account). The default `repo` and `read:org` scopes cover repositories and membership. To also adopt organization webhooks or teams, add their scopes:

```
gh auth refresh -s admin:org_hook,admin:org
```

Without those scopes, org webhooks and teams are skipped with a note rather than failing the run.

## 3. Run your first onboarding

Point TerraLift at a scope. The `--scope` value is the project ID for GCP, the account ID for AWS, or the subscription ID for Azure.

```
terralift onboard --cloud gcp --scope my-project-id
```

A good first run is a dry run. It enumerates and reports without generating a repository, so you can see what TerraLift found before it does any real work.

```
terralift onboard --cloud gcp --scope my-project-id --dry-run
```

To narrow a large environment, target specific containers. On Azure these are resource groups, and on GCP or AWS they are the natural sub-scopes.

```
terralift onboard --cloud azure --scope <sub-id> --resource-groups rg-app,rg-data
```

Everything lands under `artifacts/<run-id>/`. Change the output root with `--artifacts`.

## 4. Read the reports first

Before you touch the generated repository, read the reports in `artifacts/<run-id>/reports/`. They tell you what happened and what needs attention.

- **coverage.md**: every resource TerraLift found, split into onboarded, intentionally excluded (managed defaults, service-created children, noise), and gaps (types it could not map). A gap count of zero means nothing manageable was missed.
- **correctness.md**: the result of the plan round-trip. It shows how many resources import cleanly and flags any that do not.
- **secrets-review.md**: configuration values that look like secrets and ship in the repository, such as a connection string or an API key in an environment variable. Move each real secret into a managed store and replace the literal with a reference before you make this repo the source of truth.
- **redactions.md**: secret values that TerraLift removed from the output entirely. These are not in the repository, so you must supply them before a from-scratch apply.
- **hygiene.md**: resources that are publicly reachable or hold broad privileges. This is a security starting point, not a blocker.

## 5. Adopt the resources

The generated stacks use import blocks, so applying them brings your existing resources under Terraform management without recreating anything.

```
cd artifacts/<run-id>/repo/live/<container>
terraform init
terraform plan
```

A clean adoption plan reads `X to import, 0 to add, 0 to change, 0 to destroy`. If you see changes, check `correctness.md` and the plan output to understand them before applying.

Configure remote state in `backend.tf` for real use, then apply:

```
terraform apply
```

After the first apply, delete `import.tf`. Import blocks are a one-time adoption step, and Terraform manages the resources normally from then on.

## 6. Wire it into CI

The repository includes `ci-pipeline.yml`, a starter that runs `terraform plan` on every pull request and gates `apply` behind a manual approval, using keyless OIDC authentication. Copy it to your CI location, for example `.github/workflows/`, and fill in the provider and backend settings for your environment.

## Next steps

- Read [Commands](commands.md) for the full flag reference and the individual phases.
- Read [How It Works](how-it-works.md) to understand the control-plane boundary, how secrets are handled, and the known limits for each cloud.
- For a portable copy you can stand up elsewhere, see clone mode in [Commands](commands.md).
