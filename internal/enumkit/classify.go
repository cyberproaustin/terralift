package enumkit

// TypeMap maps a cloud-native resource type (e.g. "compute.googleapis.com/Instance",
// "ec2:instance", "Microsoft.Compute/virtualMachines") to its Terraform type.
// An unmapped native type resolves to "" — which callers treat as a coverage gap,
// never as "no resource." A provider composes its full map from smaller ones with
// Merge, then classifies each enumerated resource with TF.
type TypeMap map[string]string

// TF returns the Terraform type for a native type, or "" if the type is unmapped
// (a genuine coverage gap).
func (m TypeMap) TF(native string) string { return m[native] }

// Has reports whether native has a Terraform mapping.
func (m TypeMap) Has(native string) bool { _, ok := m[native]; return ok }

// Merge returns a new TypeMap combining the receiver with each of others; later
// maps win on key collision. Used to layer a full-sweep coverage map over a
// hand-curated core map without mutating either.
func (m TypeMap) Merge(others ...TypeMap) TypeMap {
	out := make(TypeMap, len(m))
	for k, v := range m {
		out[k] = v
	}
	for _, o := range others {
		for k, v := range o {
			out[k] = v
		}
	}
	return out
}
