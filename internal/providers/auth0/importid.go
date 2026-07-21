package auth0

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. The Phase-A Auth0 set is
// deliberately COMPOSITE-FREE: every resource imports by a single bare token — an opaque id
// (client by client_id, resource_server/connection/role/action/organization/client_grant/
// log_stream by id), a template name (email_template), or a stable singleton sentinel (tenant/
// branding/attack_protection/prompt/guardian/email_provider — the provider discards the id and
// reads the tenant-wide object, so a stable sentinel keeps re-runs idempotent). Escaping keeps
// parity with the other providers (email-template names and Auth0 EL/Liquid config can carry
// ${…}/%{…}, though the ids/names here are constrained).
//
// The DEFERRED relationship plane (auth0_connection_client, auth0_organization_connection/
// member, auth0_role_permission [3-part], auth0_trigger_actions, …) joins its parts with `::`
// (DOUBLE-COLON, not `/` or `:`) at varying depth — when that increment lands it needs an
// explicit per-TF-type switch here, exactly like Okta's rawImportID. Never infer the separator.
func deriveImportID(r *model.Resource) string {
	s, _ := r.Properties["token"].(string)
	return util.EscapeHCLTemplate(s)
}
