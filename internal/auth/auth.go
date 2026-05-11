// Package auth implements OIDC token verification and RBAC for the
// cadoo-api surface. SAML SSO is delegated to Dex (or any OIDC-fronted
// IdP); Cadoo only ever speaks OIDC.
package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Role is one of the four built-in tiers. Higher tiers strictly contain
// lower tiers (RoleOwner can do anything RoleAdmin can, etc.).
type Role string

// Role tiers.
const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

// Allows reports whether r is at least as privileged as minRole.
func (r Role) Allows(minRole Role) bool { return rank(r) >= rank(minRole) }

func rank(r Role) int {
	switch r {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleMember:
		return 2
	case RoleViewer:
		return 1
	}
	return 0
}

// Claims is the subset of OIDC claims Cadoo cares about. Custom claims (org,
// roles) come from the IdP — point Dex's group→claim mapping at these names.
type Claims struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email"`
	Name    string   `json:"name"`
	Org     string   `json:"org"`
	Roles   []string `json:"roles"`
}

// HasRole reports whether the claims include any role at or above minRole.
func (c *Claims) HasRole(minRole Role) bool {
	for _, r := range c.Roles {
		if Role(r).Allows(minRole) {
			return true
		}
	}
	return false
}

// Verifier wraps an OIDC IDTokenVerifier configured for one issuer + audience.
type Verifier struct {
	v *oidc.IDTokenVerifier
}

// NewVerifier discovers the OIDC provider at issuer and returns a Verifier
// scoped to clientID. ctx is only used for discovery.
func NewVerifier(ctx context.Context, issuer, clientID string) (*Verifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	return &Verifier{v: provider.Verifier(&oidc.Config{ClientID: clientID})}, nil
}

// Verify validates the raw bearer token's signature, expiry, and audience,
// then decodes Cadoo's claim set.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	tok, err := v.v.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	var c Claims
	if err := tok.Claims(&c); err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	return &c, nil
}
