package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/cyberproaustin/terralift/internal/enumkit"
)

// azBin resolves the az executable (az.cmd on Windows).
func azBin() string {
	if runtime.GOOS == "windows" {
		return "az.cmd"
	}
	return "az"
}

// azEnv returns the child environment for an `az` invocation. Azure Resource Graph (`az graph`)
// ships as the `resource-graph` CLI extension; on a machine that doesn't have it yet, the CLI tries
// to install it and PROMPTS "install extension? (y/n)" — which EOFs against our non-interactive
// subprocess stdin and crashes the command with "EOFError: EOF when reading a line". Opting into
// silent auto-install (the env-var form of `az config set
// extension.use_dynamic_install=yes_without_prompt`) fetches any missing extension without a prompt.
func azEnv() []string {
	return append(os.Environ(), "AZURE_EXTENSION_USE_DYNAMIC_INSTALL=yes_without_prompt")
}

// runAz runs `az <args...> -o json --only-show-errors` and unmarshals stdout.
func runAz(ctx context.Context, v any, args ...string) error {
	full := append(append([]string{}, args...), "-o", "json", "--only-show-errors")
	cmd := exec.CommandContext(ctx, azBin(), full...)
	cmd.Env = azEnv()
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
	return enumkit.Paginate(func(skip string) ([]map[string]any, string, error) {
		args := []string{"graph", "query", "-q", query, "--first", "1000"}
		if subscription != "" {
			args = append(args, "--subscriptions", subscription)
		}
		if skip != "" {
			args = append(args, "--skip-token", skip)
		}
		var resp graphResponse
		if err := runAz(ctx, &resp, args...); err != nil {
			return nil, "", err
		}
		return resp.Data, resp.SkipToken, nil
	})
}
