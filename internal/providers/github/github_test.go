package github

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/model"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestGhAPIListFlattensPages locks the concatenated-array decoding: gh --paginate
// emits one JSON array PER PAGE (arrays concatenated, not merged), and ghAPIList
// must flatten them into a single slice.
func TestGhAPIListFlattensPages(t *testing.T) {
	orig := ghExec
	t.Cleanup(func() { ghExec = orig })
	ghExec = func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte(`[{"name":"a"}][{"name":"b"},{"name":"c"}]`), nil
	}
	got, err := ghAPIList[repo](context.Background(), "user/repos")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "a" || got[2].Name != "c" {
		t.Errorf("expected 3 flattened repos a,b,c; got %+v", got)
	}
}

func TestDeriveImportID(t *testing.T) {
	r := &model.Resource{TFType: "github_repository", Name: "my-repo", ID: "owner/my-repo"}
	if got := deriveImportID(r); got != "my-repo" {
		t.Errorf("github_repository import id = %q, want the bare repo name", got)
	}
	wh := &model.Resource{TFType: "github_repository_webhook", Properties: map[string]any{"repo": "my-repo", "hook_id": "42"}}
	if got := deriveImportID(wh); got != "my-repo/42" {
		t.Errorf("webhook import id = %q, want my-repo/42", got)
	}
	bp := &model.Resource{TFType: "github_branch_protection", Properties: map[string]any{"repo": "my-repo", "pattern": "main"}}
	if got := deriveImportID(bp); got != "my-repo:main" {
		t.Errorf("branch protection import id = %q, want my-repo:main", got)
	}
	mem := &model.Resource{TFType: "github_membership", Properties: map[string]any{"org": "my-org", "username": "alice"}}
	if got := deriveImportID(mem); got != "my-org:alice" {
		t.Errorf("membership import id = %q, want my-org:alice", got)
	}
}

func TestAuthorWebhookURLs(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/generated.tf"
	// generate-config-out nulls the REQUIRED configuration.url (marks it sensitive).
	src := `resource "github_repository_webhook" "r_hook_1" {
  active     = false
  repository = "r"
  configuration {
    content_type = "json"
    url          = null # sensitive
  }
}`
	writeFile(t, p, src)
	n := authorWebhookURLs(p, map[string]string{"github_repository_webhook.r_hook_1": "https://example.com/h"})
	if n != 1 {
		t.Fatalf("authored %d webhook urls, want 1", n)
	}
	out := readFile(t, p)
	if !strings.Contains(out, `= "https://example.com/h"`) {
		t.Errorf("webhook url not authored from live value:\n%s", out)
	}
	if strings.Contains(out, "null") {
		t.Errorf("null url not replaced:\n%s", out)
	}
}

func TestPruneGeneratedHCL(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/generated.tf"
	src := `resource "github_repository" "r" {
  name = "r"
  etag = "W/\"abc\""
  fork = "false"
  has_issues = true
}`
	writeFile(t, p, src)
	if n := pruneGeneratedHCL(p); n != 2 {
		t.Errorf("pruned %d lines, want 2 (etag + fork)", n)
	}
	out := readFile(t, p)
	if strings.Contains(out, "etag") || strings.Contains(out, "fork") {
		t.Errorf("etag/fork not pruned:\n%s", out)
	}
	if !strings.Contains(out, "has_issues") {
		t.Errorf("pruning removed a real attribute:\n%s", out)
	}
}

func TestAuthorRepoAttrs(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/generated.tf"
	src := `resource "github_repository" "r" {
  name = "r"
  has_issues = true
}`
	writeFile(t, p, src)
	if n := authorRepoAttrs(p, map[string]bool{"r": true}); n != 1 {
		t.Fatalf("authored %d blocks, want 1", n)
	}
	out := readFile(t, p)
	if !strings.Contains(out, "has_downloads = true") {
		t.Errorf("has_downloads not authored from live value:\n%s", out)
	}
	if !strings.Contains(out, "ignore_vulnerability_alerts_during_read = false") {
		t.Errorf("provider read-flag not authored:\n%s", out)
	}
	// Idempotent: a second pass must not duplicate the attributes.
	if n := authorRepoAttrs(p, map[string]bool{"r": true}); n != 0 {
		t.Errorf("second authoring pass edited %d blocks, want 0 (already present)", n)
	}
	if strings.Count(readFile(t, p), "has_downloads") != 1 {
		t.Errorf("has_downloads duplicated on re-author:\n%s", readFile(t, p))
	}
}
