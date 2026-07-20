// Package auth verifies Authentik-issued OIDC tokens and projects the caller's
// identity (sub/email/groups) onto the Fiber request context. It is a pure
// verifier: the interactive Authorization Code + PKCE flow, cookies and refresh
// live in the SvelteKit BFF (see docs/planning/01-architecture.md §3.1). The API
// trusts only the bearer token forwarded on each request, validated against
// Authentik's JWKS.
//
// This package deliberately extracts groups but enforces no access-control rules
// — ACL enforcement is M3 (03-data-model.md §2). Groups are placed on the request
// context so M3 consumes them without touching this middleware.
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/coreos/go-oidc/v3/oidc"
)

// Verifier validates signed OIDC tokens: signature (against Authentik's JWKS),
// issuer, audience and expiry. It is safe for concurrent use; the underlying
// go-oidc verifier caches the JWKS with the key set's server-provided TTL.
type Verifier struct {
	verifier *oidc.IDTokenVerifier
}

// New builds a Verifier by discovering Authentik's OIDC configuration
// (.well-known/openid-configuration) rather than hardcoding endpoints, so the
// flow tracks Authentik config changes automatically (ticket suggestion).
//
// Behind a reverse proxy the external issuer (matching the token `iss`) differs
// from the internal discovery/JWKS host reachable by the API. In that case we
// fetch discovery from the internal URL but still validate `iss` against the
// external issuer via oidc.InsecureIssuerURLContext.
func New(ctx context.Context, cfg config.Config) (*Verifier, error) {
	if !cfg.AuthEnabled() {
		return nil, errors.New("auth: OIDC issuer not configured")
	}
	if cfg.OIDCAudience == "" {
		return nil, errors.New("auth: OIDC audience not configured (required to validate aud)")
	}

	discoveryURL := cfg.OIDCDiscoveryURL()
	discoveryCtx := ctx
	if discoveryURL != cfg.OIDCIssuer {
		// Split-horizon: fetch discovery internally, but expect the external
		// issuer in the discovery document and in token `iss` claims.
		discoveryCtx = oidc.InsecureIssuerURLContext(ctx, cfg.OIDCIssuer)
	}

	provider, err := oidc.NewProvider(discoveryCtx, discoveryURL)
	if err != nil {
		return nil, fmt.Errorf("auth: discover OIDC provider: %w", err)
	}

	// ClientID is the expected audience: go-oidc rejects tokens whose `aud`
	// does not contain it, closing the cross-app token-replay hole.
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.OIDCAudience})
	return &Verifier{verifier: verifier}, nil
}

// VerifierFrom wraps a preconfigured go-oidc verifier. Useful when the key set
// is obtained out of band of discovery — a pre-fetched JWKS, or a static key set
// in tests — without reaching Authentik over the network.
func VerifierFrom(idv *oidc.IDTokenVerifier) *Verifier {
	return &Verifier{verifier: idv}
}

// Verify checks the raw JWT and returns its normalized claims. It never returns
// or logs the token itself; callers translate any error into a 401.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}

	var claims Claims
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	claims.normalize()

	if claims.Subject == "" {
		return nil, errors.New("token missing sub claim")
	}
	return &claims, nil
}
