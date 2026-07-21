package mackerel

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. Mackerel has two import
// shapes:
//   - a BARE token — a service name, or an opaque string id (monitor/channel/notification_group/
//     dashboard/aws_integration/downtime/alert_group_setting);
//   - a COLON composite <service>:<role> for a role (service + role names, neither of which may
//     contain a colon, so the join is unambiguous).
//
// Escaping neutralizes any ${…}/%{…} before hcl.ImportBlock's %q (which does not), matching the
// other providers. VERIFY at Phase B: mackerel_dashboard has no documented import section, and the
// mackerel_service_metadata docs contradict themselves on ':' vs '/'.
func deriveImportID(r *model.Resource) string {
	if svc, ok := r.Properties["service"].(string); ok {
		role, _ := r.Properties["role"].(string)
		return util.EscapeHCLTemplate(svc + ":" + role)
	}
	tok, _ := r.Properties["token"].(string)
	return util.EscapeHCLTemplate(tok)
}
