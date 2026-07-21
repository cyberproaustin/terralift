package newrelic

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// nrID is a NerdGraph id that tolerates both a JSON string and a bare JSON number — New
// Relic returns ids as numbers on some fields (policy/condition/workload/obfuscation) and
// strings on others. A plain string field would fail the whole decode on a numeric id.
type nrID string

func (d *nrID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*d = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*d = nrID(s)
		return nil
	}
	*d = nrID(b) // bare number → its literal text
	return nil
}

func (d nrID) String() string { return string(d) }

// entityFilter builds the entitySearch `query` string. The account id is a validated int
// we control (not attacker input), so composing it into the filter STRING (the value of
// the $query GraphQL variable) is safe — the GraphQL query TEXT itself stays static.
//
// SECURITY: `filter` MUST be a package-internal compile-time literal (see the callers in
// enumerate.go — "type = 'DASHBOARD'", "domain = 'SYNTH' AND type = 'MONITOR'", ...). Never
// pass user- or server-derived text here: an injected `OR accountId = <other>` would broaden
// the entitySearch past the intended account.
func entityFilter(filter string, acct int) string {
	return fmt.Sprintf("%s AND accountId = %d", filter, acct)
}

// --- entitySearch (dashboards / synthetics / workloads / key transactions) -------------

const qEntitySearch = `query($query: String!, $cursor: String) {
  actor {
    entitySearch(query: $query) {
      results(cursor: $cursor) {
        entities {
          guid
          name
          entityType
          ... on SyntheticMonitorEntityOutline { monitorType }
          ... on DashboardEntityOutline { dashboardParentGuid }
        }
        nextCursor
      }
    }
  }
}`

type nrEntity struct {
	GUID                string `json:"guid"`
	Name                string `json:"name"`
	EntityType          string `json:"entityType"`
	MonitorType         string `json:"monitorType"`         // synthetics only
	DashboardParentGUID string `json:"dashboardParentGuid"` // dashboards only
}

func extractEntities(data json.RawMessage) ([]nrEntity, string, error) {
	var r struct {
		Actor struct {
			EntitySearch struct {
				Results struct {
					Entities   []nrEntity `json:"entities"`
					NextCursor string     `json:"nextCursor"`
				} `json:"results"`
			} `json:"entitySearch"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, "", &nerdgraphError{msg: "decode entitySearch: " + err.Error()}
	}
	return r.Actor.EntitySearch.Results.Entities, r.Actor.EntitySearch.Results.NextCursor, nil
}

// --- alert policies --------------------------------------------------------------------

const qAlertPolicies = `query($acct: Int!, $cursor: String) {
  actor { account(id: $acct) { alerts {
    policiesSearch(cursor: $cursor) {
      policies { id name }
      nextCursor
    }
  } } }
}`

type nrIDName struct {
	ID   nrID   `json:"id"`
	Name string `json:"name"`
}

func extractPolicies(data json.RawMessage) ([]nrIDName, string, error) {
	var r struct {
		Actor struct {
			Account struct {
				Alerts struct {
					PoliciesSearch struct {
						Policies   []nrIDName `json:"policies"`
						NextCursor string     `json:"nextCursor"`
					} `json:"policiesSearch"`
				} `json:"alerts"`
			} `json:"account"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, "", &nerdgraphError{msg: "decode policiesSearch: " + err.Error()}
	}
	s := r.Actor.Account.Alerts.PoliciesSearch
	return s.Policies, s.NextCursor, nil
}

// --- NRQL alert conditions -------------------------------------------------------------

const qNRQLConditions = `query($acct: Int!, $cursor: String) {
  actor { account(id: $acct) { alerts {
    nrqlConditionsSearch(cursor: $cursor) {
      nrqlConditions { id name policyId type }
      nextCursor
    }
  } } }
}`

type nrNRQLCondition struct {
	ID       nrID   `json:"id"`
	Name     string `json:"name"`
	PolicyID nrID   `json:"policyId"`
	Type     string `json:"type"` // STATIC | BASELINE — part of the import id
}

func extractNRQLConditions(data json.RawMessage) ([]nrNRQLCondition, string, error) {
	var r struct {
		Actor struct {
			Account struct {
				Alerts struct {
					NrqlConditionsSearch struct {
						NrqlConditions []nrNRQLCondition `json:"nrqlConditions"`
						NextCursor     string            `json:"nextCursor"`
					} `json:"nrqlConditionsSearch"`
				} `json:"alerts"`
			} `json:"account"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, "", &nerdgraphError{msg: "decode nrqlConditionsSearch: " + err.Error()}
	}
	s := r.Actor.Account.Alerts.NrqlConditionsSearch
	return s.NrqlConditions, s.NextCursor, nil
}

// --- alert muting rules (no cursor) ----------------------------------------------------

const qMutingRules = `query($acct: Int!) {
  actor { account(id: $acct) { alerts {
    mutingRules { id name }
  } } }
}`

func decodeMutingRules(data json.RawMessage) ([]nrIDName, error) {
	var r struct {
		Actor struct {
			Account struct {
				Alerts struct {
					MutingRules []nrIDName `json:"mutingRules"`
				} `json:"alerts"`
			} `json:"account"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, &nerdgraphError{msg: "decode mutingRules: " + err.Error()}
	}
	return r.Actor.Account.Alerts.MutingRules, nil
}

// --- aiNotifications destinations / channels -------------------------------------------

const qDestinations = `query($acct: Int!, $cursor: String) {
  actor { account(id: $acct) { aiNotifications {
    destinations(cursor: $cursor) {
      entities { id name type }
      nextCursor
    }
  } } }
}`

const qChannels = `query($acct: Int!, $cursor: String) {
  actor { account(id: $acct) { aiNotifications {
    channels(cursor: $cursor) {
      entities { id name type destinationId }
      nextCursor
    }
  } } }
}`

type nrNotifEntity struct {
	ID            nrID   `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	DestinationID nrID   `json:"destinationId"` // channels only
}

func extractDestinations(data json.RawMessage) ([]nrNotifEntity, string, error) {
	return extractAINotif(data, "destinations")
}

func extractChannels(data json.RawMessage) ([]nrNotifEntity, string, error) {
	return extractAINotif(data, "channels")
}

func extractAINotif(data json.RawMessage, field string) ([]nrNotifEntity, string, error) {
	var r struct {
		Actor struct {
			Account struct {
				AiNotifications map[string]struct {
					Entities   []nrNotifEntity `json:"entities"`
					NextCursor string          `json:"nextCursor"`
				} `json:"aiNotifications"`
			} `json:"account"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, "", &nerdgraphError{msg: "decode aiNotifications." + field + ": " + err.Error()}
	}
	f := r.Actor.Account.AiNotifications[field]
	return f.Entities, f.NextCursor, nil
}

// --- aiWorkflows workflows -------------------------------------------------------------

const qWorkflows = `query($acct: Int!, $cursor: String) {
  actor { account(id: $acct) { aiWorkflows {
    workflows(cursor: $cursor) {
      entities { id name }
      nextCursor
    }
  } } }
}`

func extractWorkflows(data json.RawMessage) ([]nrIDName, string, error) {
	var r struct {
		Actor struct {
			Account struct {
				AiWorkflows struct {
					Workflows struct {
						Entities   []nrIDName `json:"entities"`
						NextCursor string     `json:"nextCursor"`
					} `json:"workflows"`
				} `json:"aiWorkflows"`
			} `json:"account"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, "", &nerdgraphError{msg: "decode aiWorkflows.workflows: " + err.Error()}
	}
	s := r.Actor.Account.AiWorkflows.Workflows
	return s.Entities, s.NextCursor, nil
}

// --- obfuscation rules + expressions (one query, no cursor) ----------------------------

const qObfuscation = `query($acct: Int!) {
  actor { account(id: $acct) { logConfigurations {
    obfuscationRules { id name }
    obfuscationExpressions { id name }
  } } }
}`

func decodeObfuscation(data json.RawMessage) (rules, exprs []nrIDName, err error) {
	var r struct {
		Actor struct {
			Account struct {
				LogConfigurations struct {
					ObfuscationRules       []nrIDName `json:"obfuscationRules"`
					ObfuscationExpressions []nrIDName `json:"obfuscationExpressions"`
				} `json:"logConfigurations"`
			} `json:"account"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, nil, &nerdgraphError{msg: "decode logConfigurations: " + err.Error()}
	}
	c := r.Actor.Account.LogConfigurations
	return c.ObfuscationRules, c.ObfuscationExpressions, nil
}

// --- workload workloadId follow-up -----------------------------------------------------
// entitySearch hands back a WorkloadEntity's guid but NOT its numeric workloadId, which the
// 3-part import composite needs. Resolve it per entity.

const qWorkloadID = `query($guid: EntityGuid!) {
  actor { entity(guid: $guid) {
    ... on WorkloadEntity { workloadId }
  } }
}`

func decodeWorkloadID(data json.RawMessage) (string, error) {
	var r struct {
		Actor struct {
			Entity struct {
				WorkloadID nrID `json:"workloadId"`
			} `json:"entity"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", &nerdgraphError{msg: "decode workloadId: " + err.Error()}
	}
	return r.Actor.Entity.WorkloadID.String(), nil
}

// --- preflight probe -------------------------------------------------------------------

const qProbe = `query($acct: Int!) {
  actor {
    user { email }
    account(id: $acct) { name }
  }
}`

type nrProbe struct {
	Actor struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
		Account *struct {
			Name string `json:"name"`
		} `json:"account"`
	} `json:"actor"`
}
