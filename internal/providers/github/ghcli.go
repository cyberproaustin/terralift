package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// ghExec runs `gh <args...>` and returns stdout. It is a package var (not a plain
// func) so tests can substitute a fake CLI; do not call it concurrently with a test
// that overrides it.
var ghExec = func(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "gh", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("gh %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// ghAPI runs `gh api <path>` and unmarshals the single JSON object into v.
func ghAPI(ctx context.Context, v any, path string) error {
	out, err := ghExec(ctx, "api", path)
	if err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	return json.Unmarshal(out, v)
}

// ghAPIList runs `gh api --paginate <path>` and flattens the result into a single
// slice. gh 2.x has no --slurp and --paginate emits one JSON array PER PAGE (arrays
// concatenated, not merged), so decode successive arrays until EOF.
func ghAPIList[T any](ctx context.Context, path string) ([]T, error) {
	out, err := ghExec(ctx, "api", "--paginate", path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var all []T
	for {
		var page []T
		if err := dec.Decode(&page); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode gh api page for %q: %w", path, err)
		}
		all = append(all, page...)
	}
	return all, nil
}

// ghToken returns the token gh is authenticated with, for the provider block.
func ghToken(ctx context.Context) (string, error) {
	out, err := ghExec(ctx, "auth", "token")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ghVersion returns the gh CLI version ("2.23.0"), or "" if gh is not on PATH.
func ghVersion(ctx context.Context) string {
	if _, err := exec.LookPath("gh"); err != nil {
		return ""
	}
	out, err := ghExec(ctx, "--version")
	if err != nil {
		return ""
	}
	// "gh version 2.23.0 (...)" -> "2.23.0"
	if f := strings.Fields(string(out)); len(f) >= 3 {
		return f[2]
	}
	return strings.TrimSpace(string(out))
}
