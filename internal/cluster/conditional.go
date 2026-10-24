package cluster

// Versioned reports whether an update to a resource of kind names the version
// it was read at, which is what lets a write be conditional on a version the
// caller read. No removal names one, so a removal never is; REST's If-Match
// pin and MCP's `version` argument are both bounded by this.
func Versioned(kind string) bool {
	switch kind {
	case "service", "node", "config", "secret":
		return true
	default:
		return false
	}
}
