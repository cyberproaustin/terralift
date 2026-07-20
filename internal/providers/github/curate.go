package github

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/cyberproaustin/terralift/internal/hcl"
	"github.com/cyberproaustin/terralift/internal/util"
)

// githubPruneRules strip attributes `terraform plan -generate-config-out` over-emits
// as computed / read-only noise on github_repository: etag (the resource's HTTP
// etag) and fork (a computed bool). Harmless to terraform, but pointless clutter in
// a managed repo.
var githubPruneRules = []*regexp.Regexp{
	regexp.MustCompile(`^\s*etag\s*=`),
	regexp.MustCompile(`^\s*fork\s*=`),
}

// pruneGeneratedHCL removes the over-emitted computed noise. Returns the line count
// removed.
func pruneGeneratedHCL(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	out, n := hcl.Prune(string(data), githubPruneRules)
	if n > 0 {
		_ = os.WriteFile(path, []byte(out), 0o644)
	}
	return n
}

// scrubGeneratedHCL redacts secret-looking values. Repositories carry no secrets;
// this becomes real when github_actions_secret / _dependabot_secret land. The
// pipeline's repo-wide secret scan is the backstop meanwhile.
func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }

// nameAttrRe pulls the repo name out of a github_repository block's `name = "..."`.
var nameAttrRe = regexp.MustCompile(`(?m)^\s*name\s*=\s*"([^"]+)"`)

// authorRepoAttrs injects the two settable attributes generate-config-out omits for
// github_repository, so adoption is plan-clean WITHOUT abandoning management (a
// lifecycle ignore_changes would stop managing them instead):
//   - has_downloads: a real repo setting the generator drops; authored from live.
//   - ignore_vulnerability_alerts_during_read: a provider read-behavior flag that
//     defaults false and always shows as a spurious add; authored as its default.
//
// hasDownloads maps repo name -> live value. Returns the count of blocks edited.
func authorRepoAttrs(path string, hasDownloads map[string]bool) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n := 0
	out, _ := hcl.WalkResourceBlocks(strings.Split(string(data), "\n"), func(typ string, block []string) ([]string, []hcl.Redaction) {
		if typ != "github_repository" {
			return block, nil
		}
		body := strings.Join(block, "\n")
		m := nameAttrRe.FindStringSubmatch(body)
		if m == nil {
			return block, nil
		}
		var ins []string
		if !strings.Contains(body, "has_downloads") {
			ins = append(ins, fmt.Sprintf("  has_downloads = %t", hasDownloads[m[1]]))
		}
		if !strings.Contains(body, "ignore_vulnerability_alerts_during_read") {
			ins = append(ins, "  ignore_vulnerability_alerts_during_read = false")
		}
		if len(ins) == 0 {
			return block, nil
		}
		// Insert before the block's closing brace (terraform fmt reorders later).
		nb := append([]string{}, block[:len(block)-1]...)
		nb = append(nb, ins...)
		nb = append(nb, block[len(block)-1])
		n++
		return nb, nil
	})
	if n > 0 {
		_ = os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
	}
	return n
}

var (
	webhookHeaderRe = regexp.MustCompile(`^resource\s+"github_repository_webhook"\s+"([^"]+)"`)
	urlAttrRe       = regexp.MustCompile(`^(\s*)url(\s*)=\s*null`)
)

// authorWebhookURLs replaces the `url = null # sensitive` line that
// generate-config-out emits for a github_repository_webhook (it wrongly treats the
// REQUIRED configuration.url as sensitive) with the live URL from the API, keyed by
// the block's "github_repository_webhook.<label>" address. Returns blocks edited.
func authorWebhookURLs(path string, urlByAddr map[string]string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n := 0
	out, _ := hcl.WalkResourceBlocks(strings.Split(string(data), "\n"), func(typ string, block []string) ([]string, []hcl.Redaction) {
		if typ != "github_repository_webhook" {
			return block, nil
		}
		m := webhookHeaderRe.FindStringSubmatch(block[0])
		if m == nil {
			return block, nil
		}
		url, ok := urlByAddr["github_repository_webhook."+m[1]]
		if !ok || url == "" {
			return block, nil
		}
		for i, l := range block {
			if sm := urlAttrRe.FindStringSubmatch(l); sm != nil {
				block[i] = sm[1] + "url" + sm[2] + "= " + fmt.Sprintf("%q", util.EscapeHCLTemplate(url))
				n++
				break
			}
		}
		return block, nil
	})
	if n > 0 {
		_ = os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
	}
	return n
}
