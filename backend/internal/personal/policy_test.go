package personal

import "testing"

func TestDefaultPolicyIsPrivateAndNonCommercial(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Validate(); err != nil {
		t.Fatalf("default policy must validate: %v", err)
	}
	if p.MaxPrivateMembers != DefaultMaxPrivateMembers {
		t.Fatalf("unexpected default member limit: %d", p.MaxPrivateMembers)
	}
}

func TestMemberLimit(t *testing.T) {
	p := DefaultPolicy()
	if err := p.ValidatePrivateMemberCount(DefaultMaxPrivateMembers); err != nil {
		t.Fatalf("limit itself must be allowed: %v", err)
	}
	if err := p.ValidatePrivateMemberCount(DefaultMaxPrivateMembers + 1); err == nil {
		t.Fatal("member count above limit must fail")
	}
}

func TestUpstreamSharingFailsClosed(t *testing.T) {
	if !CanUseUpstream(RoleOwner, ShareOwnerOnly) {
		t.Fatal("owner should be allowed to use owner-only upstream")
	}
	if CanUseUpstream(RoleMember, ShareOwnerOnly) {
		t.Fatal("private member must not use owner-only upstream")
	}
	if !CanUseUpstream(RoleMember, SharePrivateMembers) {
		t.Fatal("private member should use explicitly shared upstream")
	}
	if CanUseUpstream(MemberRole("unknown"), SharePrivateMembers) {
		t.Fatal("unknown role must fail closed")
	}
	if CanUseUpstream(RoleMember, UpstreamShareScope("unknown")) {
		t.Fatal("unknown share scope must fail closed")
	}
}

func TestCommercialFeaturesAreRejected(t *testing.T) {
	p := DefaultPolicy()
	p.PaymentsEnabled = true
	if err := p.Validate(); err == nil {
		t.Fatal("payments must be rejected in personal mode")
	}
}
