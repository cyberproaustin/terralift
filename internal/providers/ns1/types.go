package ns1

// tfTypeMap maps a native NS1 resource key ("ns1:<kind>") to its Terraform type in
// the ns1/ns1 provider.
var tfTypeMap = map[string]string{
	"ns1:zone":          "ns1_zone",
	"ns1:record":        "ns1_record",
	"ns1:monitoringjob": "ns1_monitoringjob",
	"ns1:datasource":    "ns1_datasource",
	"ns1:datafeed":      "ns1_datafeed",
	"ns1:notifylist":    "ns1_notifylist",
	"ns1:team":          "ns1_team",
	"ns1:user":          "ns1_user",
	"ns1:apikey":        "ns1_apikey",
	"ns1:tsigkey":       "ns1_tsigkey",
}

func tfType(native string) string { return tfTypeMap[native] }
