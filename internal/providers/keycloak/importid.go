package keycloak

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. Keycloak import ids are
// REALM-PREFIXED: the realm imports by its bare NAME; everything else is a 2-part
// <realm>/<leaf>. There is NO 3-part id in the Phase-A set — crucially keycloak_role (realm AND
// client roles) is 2-part <realm>/<role_id> (the role_id is a globally-unique UUID; the client
// UUID is a resource attribute, not part of the import id). The leaf is a UUID for
// client/role/group/scope/flow/federation and an alias for idp/required_action — enumerate.go
// stored the correct field. Escaping is load-bearing: realm/client/role names can contain a
// literal `$` that terraform would otherwise interpolate.
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	if r.TFType == "keycloak_realm" {
		return p("token") // bare realm name
	}
	// 2-part <realm>/<leaf> for every other Phase-A type.
	return p("left") + "/" + p("right")
}
