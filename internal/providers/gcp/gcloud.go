package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// gcloudBin resolves the gcloud executable. On Windows the launcher is
// gcloud.cmd (the bare `gcloud`/gcloud.ps1 has broken under some PS 7.3.x).
func gcloudBin() string {
	if runtime.GOOS == "windows" {
		return "gcloud.cmd"
	}
	return "gcloud"
}

// runGcloudJSON runs `gcloud <args...> --format=json --quiet`, unmarshaling
// stdout into v. gcloud auto-paginates for --format=json (returns all results).
// A non-zero exit becomes an error carrying stderr; empty output leaves v as-is.
func runGcloudJSON(ctx context.Context, v any, args ...string) error {
	full := append(append([]string{}, args...), "--format=json", "--quiet")
	cmd := exec.CommandContext(ctx, gcloudBin(), full...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("gcloud %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return fmt.Errorf("gcloud %s: %w", strings.Join(args, " "), err)
	}
	if len(out) == 0 {
		return nil
	}
	return json.Unmarshal(out, v)
}
