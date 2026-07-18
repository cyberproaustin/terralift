package reconcile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// SecretsReview is the FLAGGING backstop for shipped app config (see
// docs/DESIGN-DECISIONS.md, ADR-001). If a shipped secret isn't being caught,
// improve this scanner — do NOT switch to blanking config maps in the redactor.
//
// SecretsReview scans onboarded stack HCL for config values that LOOK like
// secrets. TerraLift intentionally SHIPS application configuration (app_settings,
// container/function env vars, connection settings) — that config is the
// highest-value part of moving to IaC and wiping it would break the apps — so
// rather than blanking it, this flags entries an operator should relocate to a
// managed secret store (Key Vault / Secrets Manager / Secret Manager) and
// reference. Unambiguous single secrets (passwords, private keys, *_access_key,
// SecureString params) are already removed by the per-provider redactor before
// this runs; this is the human-review backstop for everything that ships.
//
// It is aware of the three shapes real secrets hide in: (1) plain `key = "value"`
// lines, (2) JSON-encoded blobs (e.g. ECS container_definitions, quoted or in a
// heredoc), and (3) split `name`/`value` block pairs (e.g. GCP Cloud Run
// `env { name = "DB_PASSWORD"  value = "…" }`).
type SecretsReview struct {
	Findings []SecretFinding
	Files    int // .tf files scanned
}

// SecretFinding is one config entry that resembles a secret and should be
// reviewed. Value is masked — the real value already lives in the shipped .tf, so
// the report only needs enough to locate it.
type SecretFinding struct {
	File     string // repo-relative path
	Line     int    // 1-indexed
	Resource string // enclosing `type.name`, or "" at file scope
	Key      string
	Reason   string
	Preview  string // masked
}

var (
	// secretKeyRe: attribute / map-key names that conventionally carry secrets.
	// The pass-segment is boundary-anchored so DB_PASS matches but "compass" does not.
	secretKeyRe = regexp.MustCompile(`(?i)((^|[_-])pass([_-]|word|phrase|wd|$)|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential|conn(ection)?[_-]?str|sas[_-]?(token|url|key)|client[_-]?secret|encrypt(ion)?[_-]?key|instrumentationkey|accountkey)`)
	// secretValueRe: values that are secret regardless of the key name. Includes
	// URI-embedded credentials (scheme://user:password@host — e.g. a DATABASE_URL
	// postgres/mysql/redis connection string), which are the most common way a real
	// password hides inside an otherwise innocuous-looking config value.
	// The URI-credential form uses [^:@/\s]* (not +) for the username so the common
	// password-only shape (redis://:pass@host, amqps://:pw@broker) is still caught.
	secretValueRe = regexp.MustCompile(`(?i)(accountkey=|sharedaccesskey=|;\s*password=|;\s*pwd=|-----BEGIN |AKIA[0-9A-Z]{16}|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}|://[^:@/\s]*:[^@/\s]+@)`)
	// high-entropy: a long unbroken base64/hex-ish blob (keys, tokens). No "/" — that
	// excludes resource paths (projects/.../..., self-links) which are long but not secret.
	entropyRe    = regexp.MustCompile(`^[A-Za-z0-9+=_-]{40,}$`)
	tfResourceRe = regexp.MustCompile(`^resource\s+"([^"]+)"\s+"([^"]+)"`)
	kvLineRe     = regexp.MustCompile(`^\s*"?([A-Za-z0-9_.\-/]+)"?\s*=\s*(.+?)\s*$`)
	heredocRe    = regexp.MustCompile(`<<-?(\w+)`)
	// A value that is a reference / expression, not a literal — never a secret by
	// key-name/entropy (but secretValueRe still runs first, so a secret embedded in
	// a jsonencode(...) argument is not missed).
	refValueRe = regexp.MustCompile(`^(var\.|local\.|module\.|data\.|jsonencode\(|\$\{|azurerm_|google_|aws_|azuread_)`)
	// nonSecretKeyRe matches attribute names that carry a resource IDENTIFIER/NAME,
	// not a secret value — e.g. secret_id names a Secret Manager secret, key_name
	// names a KMS key. A literal secret in such a field is still caught first by
	// secretValueRe; this only suppresses the key-name heuristic's false positives.
	nonSecretKeyRe = regexp.MustCompile(`(?i)^(secret_id|secret_name|key_name|kms_key_name|key_ring|crypto_key_id|key_id)$`)
)

// ScanSecrets walks root (the repo dir) for *.tf and returns flagged entries.
func ScanSecrets(root string) SecretsReview {
	var rep SecretsReview
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".tf") {
			return nil
		}
		switch filepath.Base(path) {
		case "import.tf", "backend.tf", "providers.tf", "provider.tf", "variables.tf":
			return nil // structural files carry no app config
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rep.Files++
		rel, _ := filepath.Rel(root, path)
		scanFile(&rep, rel, string(data))
		return nil
	})
	return rep
}

func scanFile(rep *SecretsReview, rel, src string) {
	resource := ""
	pendingName := "" // most recent `name = "…"` for split name/value pairs
	lines := strings.Split(src, "\n")
	add := func(line int, key, reason, val string) {
		rep.Findings = append(rep.Findings, SecretFinding{
			File: rel, Line: line + 1, Resource: resource, Key: key, Reason: reason, Preview: mask(val),
		})
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if m := tfResourceRe.FindStringSubmatch(trimmed); m != nil {
			resource, pendingName = m[1]+"."+m[2], ""
			continue
		}
		if strings.Contains(line, "TerraLift: scrubbed") { // already redacted
			continue
		}
		m := kvLineRe.FindStringSubmatch(line)
		if m == nil {
			if trimmed == "}" || trimmed == "" {
				pendingName = ""
			}
			// Raw backstop: catch a secret pattern embedded in any non-kv line
			// (JSON fragment, stray literal) with the specific value regex.
			if secretValueRe.MatchString(line) {
				add(i, "(inline)", reasonValuePattern, trimmed)
			}
			continue
		}
		key, rawVal := m[1], strings.TrimSpace(m[2])
		// Strip a trailing "# …" comment (generate-config-out / TerraLift add them,
		// e.g. `value = null # sensitive`) — quote-aware, so a "#" inside a real
		// string value is preserved. This stops `= null # …` redacted attributes from
		// being flagged as if they held a literal secret.
		if code, _ := splitComment(rawVal); strings.TrimSpace(code) != "" {
			rawVal = strings.TrimSpace(code)
		}

		// (2a) Heredoc value: collect the body and scan it as JSON, or line-by-line
		// for raw patterns if it isn't JSON (e.g. a PEM block).
		if hm := heredocRe.FindStringSubmatch(rawVal); hm != nil {
			var body []string
			j := i + 1
			for ; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == hm[1] {
					break
				}
				body = append(body, lines[j])
			}
			bodyStr := strings.TrimSpace(strings.Join(body, "\n"))
			if json.Valid([]byte(bodyStr)) {
				for _, h := range scanJSON(bodyStr) {
					add(i, h.key, h.reason, h.val)
				}
			} else {
				for bi, bl := range body {
					if secretValueRe.MatchString(bl) {
						add(i+1+bi, "(inline)", reasonValuePattern, strings.TrimSpace(bl))
					}
				}
			}
			i, pendingName = j, ""
			continue
		}

		val := rawVal
		if uq, err := strconv.Unquote(rawVal); err == nil {
			val = uq
		}

		// (2b) One-line JSON blob value (e.g. container_definitions = "[…]").
		if looksJSON(val) {
			for _, h := range scanJSON(val) {
				add(i, h.key, h.reason, h.val)
			}
			pendingName = ""
			continue
		}

		// (3) Split name/value pair: remember a name, then judge the paired value
		// using that name (so `value = "…"` under `name = "DB_PASSWORD"` is flagged).
		if key == "name" {
			pendingName = val
			continue
		}
		effKey := key
		if key == "value" && pendingName != "" {
			effKey = pendingName
		}
		if r := secretReason(effKey, val); r != "" {
			add(i, effKey, r, val)
		}
		if key == "value" {
			pendingName = ""
		}
	}
}

const (
	reasonValuePattern = "value matches a secret pattern (connection string / key / token)"
	reasonKeyName      = "key name conventionally holds a secret"
	reasonEntropy      = "high-entropy value (possible key or token)"
)

// secretReason returns why (key, val) looks like a secret, or "" if it doesn't.
// The value-pattern check runs FIRST — before the reference/jsonencode skip — so a
// literal secret embedded in a jsonencode(...) argument is still caught.
func secretReason(key, val string) string {
	if val == "" || val == "null" {
		return ""
	}
	if secretValueRe.MatchString(val) {
		return reasonValuePattern
	}
	if refValueRe.MatchString(val) { // a reference/expression: no key/entropy heuristic
		return ""
	}
	if nonSecretKeyRe.MatchString(key) { // an identifier/name attribute, not a secret value
		return ""
	}
	if secretKeyRe.MatchString(key) && looksSecretValue(val) {
		return reasonKeyName
	}
	if entropyRe.MatchString(val) {
		return reasonEntropy
	}
	return ""
}

// benignValueRe matches values that are clearly NOT secrets even under a
// secret-ish key: booleans, numbers, and short bare enum tokens (a Terraform schema
// attribute like `http_tokens = "required"` or `get_password_data = false` should
// not be flagged just because its NAME contains "token"/"password").
var benignValueRe = regexp.MustCompile(`^([a-z]+|[A-Z]+|[A-Z][a-z]+|-?\d+(\.\d+)?|true|false)$`)

// looksSecretValue gates the key-name heuristic on the value being plausibly a
// secret: not a bool/number/short bare enum. A real secret is long or has the mix
// of case/digits/punctuation that a bare enum word lacks.
func looksSecretValue(val string) bool {
	if benignValueRe.MatchString(val) && len(val) <= 12 {
		return false
	}
	return len(val) > 8 || strings.ContainsAny(val, "!@#$%^&*()+=/\\:;?")
}

func looksJSON(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 || (s[0] != '{' && s[0] != '[') {
		return false
	}
	return json.Valid([]byte(s))
}

type jsonHit struct{ key, val, reason string }

// scanJSON parses a JSON blob and flags string values that look like secrets,
// understanding the {"name":…,"value":…} env-pair shape (ECS container_definitions).
func scanJSON(raw string) []jsonHit {
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return nil
	}
	var hits []jsonHit
	walkJSON(v, &hits)
	return hits
}

func walkJSON(v any, hits *[]jsonHit) {
	switch t := v.(type) {
	case map[string]any:
		nmRaw, hasName := t["name"]
		nm, _ := nmRaw.(string)
		if hasName {
			if vv, ok := t["value"].(string); ok {
				if r := secretReason(nm, vv); r != "" {
					*hits = append(*hits, jsonHit{nm, vv, r})
				}
			}
		}
		for k, val := range t {
			s, ok := val.(string)
			if !ok {
				walkJSON(val, hits)
				continue
			}
			if k == "name" || (k == "value" && hasName) {
				continue // the name/value pair above covers these
			}
			if r := secretReason(k, s); r != "" {
				*hits = append(*hits, jsonHit{k, s, r})
			}
		}
	case []any:
		for _, e := range t {
			walkJSON(e, hits)
		}
	}
}

// mask keeps a short lead so a human can find the value, hiding the rest.
func mask(v string) string {
	if len(v) <= 8 {
		return "********"
	}
	return v[:6] + "…(" + strconv.Itoa(len(v)) + " chars)"
}
