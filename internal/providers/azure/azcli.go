package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// azBin resolves the az executable (az.cmd on Windows).
func azBin() string {
	if runtime.GOOS == "windows" {
		return "az.cmd"
	}
	return "az"
}

// runAz runs `az <args...> -o json --only-show-errors` and unmarshals stdout.
func runAz(ctx context.Context, v any, args ...string) error {
	full := append(append([]string{}, args...), "-o", "json", "--only-show-errors")
	cmd := exec.CommandContext(ctx, azBin(), full...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("az %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return fmt.Errorf("az %s: %w", strings.Join(args, " "), err)
	}
	if len(out) == 0 {
		return nil
	}
	return json.Unmarshal(out, v)
}

// graphResponse is the shape of `az graph query -o json`.
type graphResponse struct {
	Data      []map[string]any `json:"data"`
	SkipToken string           `json:"skip_token"`
}

// graphQuery runs an Azure Resource Graph (KQL) query with SkipToken paging.
// -First max is 1000; 'id' must stay in the projection for a SkipToken to return.
func graphQuery(ctx context.Context, subscription, query string) ([]map[string]any, error) {
	var all []map[string]any
	skip := ""
	for {
		args := []string{"graph", "query", "-q", query, "--first", "1000"}
		if subscription != "" {
			args = append(args, "--subscriptions", subscription)
		}
		if skip != "" {
			args = append(args, "--skip-token", skip)
		}
		var resp graphResponse
		if err := runAz(ctx, &resp, args...); err != nil {
			return nil, err
		}
		all = append(all, resp.Data...)
		if resp.SkipToken == "" {
			break
		}
		skip = resp.SkipToken
	}
	return all, nil
}
