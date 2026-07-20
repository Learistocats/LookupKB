package auth

// Claims is the subset of the verified token we project. Only these fields are
// trusted; everything else in the token is ignored. Parsing is defensive: a user
// in zero groups yields an empty (non-nil) slice, and an arbitrarily large group
// list is accepted without special-casing — the middleware never logs group
// contents, so size is bounded only by the token itself.
type Claims struct {
	Subject           string   `json:"sub"`
	Email             string   `json:"email"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	Groups            []string `json:"groups"`
}

// normalize makes the claims safe to consume: groups is never nil and carries no
// empty entries, so downstream ACL code (M3) can intersect it without guards.
func (c *Claims) normalize() {
	if c.Groups == nil {
		c.Groups = []string{}
		return
	}
	cleaned := c.Groups[:0]
	for _, g := range c.Groups {
		if g != "" {
			cleaned = append(cleaned, g)
		}
	}
	c.Groups = cleaned
}

// DisplayName is the best human-readable label available in the token, falling
// back through name → preferred_username → email → sub so a row is always
// meaningfully labelled.
func (c *Claims) DisplayName() string {
	switch {
	case c.Name != "":
		return c.Name
	case c.PreferredUsername != "":
		return c.PreferredUsername
	case c.Email != "":
		return c.Email
	default:
		return c.Subject
	}
}
