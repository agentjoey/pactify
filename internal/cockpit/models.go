package cockpit

// ModelDiscovery contains only public selection metadata. Harness configuration
// and provider/authentication fields must never be retained here.
type ModelDiscovery struct {
	Warning      string
	Items        []string
	Efforts      []string
	DefaultModel string
}
