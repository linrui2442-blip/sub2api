// Package personal defines policy contracts for the private Personal Edition.
package personal

import "fmt"

const (
	// ModeName is the target run-mode name used by Personal Edition.
	// It is intentionally defined here before wiring it into the upstream
	// config package so policy code can be tested independently.
	ModeName = "personal"

	// DefaultMaxPrivateMembers excludes the owner/admin account.
	DefaultMaxPrivateMembers = 10
)

// MemberRole describes who is making a gateway request.
type MemberRole string

const (
	RoleOwner  MemberRole = "owner"
	RoleMember MemberRole = "member"
)

// UpstreamShareScope controls which local identities may route requests
// through an upstream account. OwnerOnly is the safe default.
type UpstreamShareScope string

const (
	ShareOwnerOnly      UpstreamShareScope = "owner_only"
	SharePrivateMembers UpstreamShareScope = "private_members"
)

// Policy captures the non-SaaS boundaries of Personal Edition.
type Policy struct {
	MaxPrivateMembers  int
	PublicRegistration bool
	SelfServiceInvite  bool
	PaymentsEnabled    bool
	TopUpEnabled       bool
	ReferralEnabled    bool
	MarketplaceEnabled bool
}

// DefaultPolicy returns the locked V1 defaults.
func DefaultPolicy() Policy {
	return Policy{
		MaxPrivateMembers:  DefaultMaxPrivateMembers,
		PublicRegistration: false,
		SelfServiceInvite:  false,
		PaymentsEnabled:    false,
		TopUpEnabled:       false,
		ReferralEnabled:    false,
		MarketplaceEnabled: false,
	}
}

// Validate checks that a policy still satisfies the Personal Edition safety
// boundary. Commercial/public features are intentionally rejected here.
func (p Policy) Validate() error {
	if p.MaxPrivateMembers < 0 {
		return fmt.Errorf("max private members cannot be negative")
	}
	if p.PublicRegistration {
		return fmt.Errorf("public registration is not allowed in personal mode")
	}
	if p.SelfServiceInvite {
		return fmt.Errorf("self-service invites are not allowed in personal mode")
	}
	if p.PaymentsEnabled || p.TopUpEnabled || p.ReferralEnabled || p.MarketplaceEnabled {
		return fmt.Errorf("commercial features are not allowed in personal mode")
	}
	return nil
}

// ValidatePrivateMemberCount enforces the configured friend/member cap. The
// owner/admin is not included in count.
func (p Policy) ValidatePrivateMemberCount(count int) error {
	if count < 0 {
		return fmt.Errorf("private member count cannot be negative")
	}
	if count > p.MaxPrivateMembers {
		return fmt.Errorf("private member count %d exceeds limit %d", count, p.MaxPrivateMembers)
	}
	return nil
}

// CanUseUpstream reports whether a local role may route requests through an
// upstream account with the given share scope. Unknown values fail closed.
func CanUseUpstream(role MemberRole, scope UpstreamShareScope) bool {
	switch role {
	case RoleOwner:
		return scope == ShareOwnerOnly || scope == SharePrivateMembers
	case RoleMember:
		return scope == SharePrivateMembers
	default:
		return false
	}
}
