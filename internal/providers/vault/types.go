package vault

// tfTypeMap maps a native Vault resource key ("vault:<kind>") to its Terraform type. The provider
// is hashicorp/vault. Phase A adopts CONFIGURATION only — mounts, auth methods, ACL policies, audit
// devices, namespaces, and the safe backend ROLES (which reference credentials but contain none).
// Secret DATA (vault_generic_secret / vault_kv_secret* / dynamic-credential reads / root+unseal
// keys) is HARD-EXCLUDED and never enumerated — reading a secret value would leak it into config.
var tfTypeMap = map[string]string{
	"vault:mount":                        "vault_mount",
	"vault:auth_backend":                 "vault_auth_backend",
	"vault:policy":                       "vault_policy",
	"vault:audit":                        "vault_audit",
	"vault:namespace":                    "vault_namespace",
	"vault:pki_secret_backend_role":      "vault_pki_secret_backend_role",
	"vault:database_secret_backend_role": "vault_database_secret_backend_role",
	"vault:aws_secret_backend_role":      "vault_aws_secret_backend_role",
	"vault:jwt_auth_backend_role":        "vault_jwt_auth_backend_role",
	"vault:approle_auth_backend_role":    "vault_approle_auth_backend_role",
	"vault:token_auth_backend_role":      "vault_token_auth_backend_role",
}

func tfType(native string) string { return tfTypeMap[native] }
