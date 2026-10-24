package auth

import (
	"net/http"
	"strings"
)

// PAT read surface. A Personal Access Token is a programmatic, read-only
// credential: it may reach ONLY the routes allowlisted here. Everything else under
// the authenticated surface (drafts, document writes, publish, token management,
// admin) is session-only and answers 403 to a PAT — deny-by-default, so a new
// route is never PAT-reachable by accident.
//
// Rules in patSurface match on the lowercased raw request path, which
// is what Fiber routes on (it is case-insensitive and does not normalise dot
// segments, so a path containing ".." is refused outright).
type surfaceRule struct {
	// prefix matches the path itself or anything below it ("/mcp" matches "/mcp"
	// and "/mcp/…", not "/mcpx").
	prefix  string
	methods []string
	// except are sub-prefixes carved out of prefix.
	except []string
}

var patSurface = []surfaceRule{
	// Versioned public read API. The account routes are never PAT-reachable.
	{prefix: "/api/v1", methods: []string{http.MethodGet, http.MethodHead}, except: []string{"/api/v1/auth"}},
	// MCP endpoint. Streamable HTTP carries JSON-RPC over POST and the
	// server→client stream over GET; its tools are read-only.
	{prefix: "/mcp", methods: []string{http.MethodGet, http.MethodPost}},
}

// PATAllowed reports whether a PAT may call method on path.
func PATAllowed(method, path string) bool {
	p := strings.ToLower(path)
	if strings.Contains(p, "..") {
		return false
	}
	for _, r := range patSurface {
		if !underPrefix(p, r.prefix) || !hasMethod(r.methods, method) {
			continue
		}
		excluded := false
		for _, e := range r.except {
			if underPrefix(p, e) {
				excluded = true
				break
			}
		}
		if !excluded {
			return true
		}
	}
	return false
}

func underPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func hasMethod(methods []string, m string) bool {
	for _, x := range methods {
		if strings.EqualFold(x, m) {
			return true
		}
	}
	return false
}
