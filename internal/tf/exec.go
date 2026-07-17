package tf

import (
	"context"
	"os/exec"
)

// Runner drives the terraform CLI in a working directory. (We can swap this for
// hashicorp/terraform-exec later; shelling out keeps the skeleton dependency-free.)
type Runner struct {
	Dir string
	Bin string
}

// New returns a Runner rooted at dir.
func New(dir string) *Runner { return &Runner{Dir: dir, Bin: "terraform"} }

func (r *Runner) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.Bin, args...)
	cmd.Dir = r.Dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Init runs `terraform init`.
func (r *Runner) Init(ctx context.Context) (string, error) {
	return r.run(ctx, "init", "-input=false", "-no-color")
}

// GenerateConfig runs `terraform plan -generate-config-out=<out>` to draft HCL
// for the import blocks in the working dir. Config generation is experimental
// (may over-emit / leak values), so callers curate the output.
func (r *Runner) GenerateConfig(ctx context.Context, outFile string) (string, error) {
	return r.run(ctx, "plan", "-input=false", "-no-color", "-generate-config-out="+outFile)
}

// Plan runs `terraform plan -out=<file>`.
func (r *Runner) Plan(ctx context.Context, outFile string) (string, error) {
	return r.run(ctx, "plan", "-input=false", "-no-color", "-out="+outFile)
}

// ShowJSON runs `terraform show -json <plan>` (feeds ParseRoundTrip).
func (r *Runner) ShowJSON(ctx context.Context, planFile string) (string, error) {
	return r.run(ctx, "show", "-json", planFile)
}
