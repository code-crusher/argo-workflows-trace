package namespaces

import "strings"

// Separator delimits the namespaces in a managed-namespace scope, e.g. "ns-1,ns-2"
// for the --managed-namespace flag of the workflow-controller and argo server.
const Separator = ","

// Parse parses a managed-namespace scope into the namespaces it contains: "" (or nil)
// means all namespaces, a single name that namespace, several comma-separated names that
// allowlist. Whitespace around names is ignored.
func Parse(scope string) []string {
	if scope == "" {
		return nil
	}
	parts := strings.Split(scope, Separator)
	namespaces := make([]string, 0, len(parts))
	for _, part := range parts {
		if name := strings.TrimSpace(part); name != "" {
			namespaces = append(namespaces, name)
		}
	}
	return namespaces
}

// Format renders a managed-namespace scope from the namespaces it contains: nil (or
// empty) means all namespaces.
func Format(namespaces []string) string {
	return strings.Join(namespaces, Separator)
}

// IsMultiple reports whether the scope names more than one namespace.
func IsMultiple(scope string) bool {
	return len(Parse(scope)) > 1
}

// ParseOrAll is like Parse but returns a single empty string when the scope covers all
// namespaces, for callers that need one concrete namespace per iteration (e.g. RBAC
// checks, where "" denotes the cluster scope).
func ParseOrAll(scope string) []string {
	if parsed := Parse(scope); len(parsed) != 0 {
		return parsed
	}
	return []string{""}
}
