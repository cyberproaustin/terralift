package pipeline

import (
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/reconcile"
)

func TestHygieneMDNotApplicable(t *testing.T) {
	// A flat provider (no IAM/exposure plane) must get an honest "not applicable"
	// report, not a "0 findings, already locked down" one.
	md := hygieneMD(reconcile.HygieneReport{}, false)
	if !strings.Contains(md, "Not applicable") {
		t.Errorf("expected a not-applicable hygiene report, got:\n%s", md)
	}
	if strings.Contains(md, "## Actions") {
		t.Errorf("not-applicable report should not render the Actions section:\n%s", md)
	}
}

func TestHygieneMDApplicableEmpty(t *testing.T) {
	// A hyperscaler with nothing to flag still renders the normal locked-down report.
	md := hygieneMD(reconcile.HygieneReport{}, true)
	if strings.Contains(md, "Not applicable") {
		t.Errorf("applicable report should not be marked not-applicable:\n%s", md)
	}
	if !strings.Contains(md, "already locked down") {
		t.Errorf("applicable empty report should say already locked down:\n%s", md)
	}
}
