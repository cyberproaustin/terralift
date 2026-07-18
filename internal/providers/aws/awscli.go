package aws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// awsBin resolves the aws executable (aws.exe on Windows).
func awsBin() string {
	if runtime.GOOS == "windows" {
		return "aws.exe"
	}
	return "aws"
}

// runAws runs `aws <args...> --output json --no-cli-pager` and unmarshals stdout.
func runAws(ctx context.Context, v any, args ...string) error {
	full := append(append([]string{}, args...), "--output", "json", "--no-cli-pager")
	cmd := exec.CommandContext(ctx, awsBin(), full...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("aws %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return fmt.Errorf("aws %s: %w", strings.Join(args, " "), err)
	}
	if len(out) == 0 {
		return nil
	}
	return json.Unmarshal(out, v)
}

// reResource is one row of an AWS Resource Explorer search result.
type reResource struct {
	ARN          string       `json:"Arn"`
	ResourceType string       `json:"ResourceType"` // "service:resource", e.g. "ec2:instance"
	Service      string       `json:"Service"`
	Region       string       `json:"Region"`
	OwningAcct   string       `json:"OwningAccountId"`
	Properties   []reProperty `json:"Properties"`
}

// reProperty is a Resource Explorer property bag entry; Name "tags" carries the
// resource's tags when the search view includes them.
type reProperty struct {
	Name string          `json:"Name"`
	Data json.RawMessage `json:"Data"`
}

type reSearchResponse struct {
	Resources []reResource `json:"Resources"`
	NextToken string       `json:"NextToken"`
}

// reSearch runs an AWS Resource Explorer (resource-explorer-2) search with
// NextToken paging. An empty query returns every resource the account's index
// can see; the aggregator index (if promoted) makes this cross-region. region,
// when set, targets the region that holds the aggregator index.
func reSearch(ctx context.Context, region, query string) ([]reResource, error) {
	var all []reResource
	token := ""
	for {
		args := []string{"resource-explorer-2", "search", "--query-string", query, "--max-results", "1000"}
		if region != "" {
			args = append(args, "--region", region)
		}
		if token != "" {
			args = append(args, "--next-token", token)
		}
		var resp reSearchResponse
		if err := runAws(ctx, &resp, args...); err != nil {
			return nil, err
		}
		all = append(all, resp.Resources...)
		if resp.NextToken == "" {
			break
		}
		token = resp.NextToken
	}
	return all, nil
}

// stsAccount returns the caller's AWS account id.
func stsAccount(ctx context.Context) (string, error) {
	var id struct {
		Account string `json:"Account"`
	}
	if err := runAws(ctx, &id, "sts", "get-caller-identity"); err != nil {
		return "", err
	}
	return id.Account, nil
}

// stsCallerARN returns the caller's identity ARN (for the preflight report).
func stsCallerARN(ctx context.Context) (string, error) {
	var id struct {
		Arn string `json:"Arn"`
	}
	if err := runAws(ctx, &id, "sts", "get-caller-identity"); err != nil {
		return "", err
	}
	return id.Arn, nil
}
