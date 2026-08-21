package service

import "context"

// UserGroupRPMOverrideEntry is a private-member RPM policy entry for a group.
// It intentionally contains no pricing or billing fields.
type UserGroupRPMOverrideEntry struct {
	UserID      int64  `json:"user_id"`
	UserName    string `json:"user_name"`
	UserEmail   string `json:"user_email"`
	UserNotes   string `json:"user_notes"`
	UserStatus  string `json:"user_status"`
	RPMOverride int    `json:"rpm_override"`
}

// GroupRPMOverrideInput is a member-specific RPM limit. A nil override clears
// the policy for that member.
type GroupRPMOverrideInput struct {
	UserID      int64 `json:"user_id"`
	RPMOverride *int  `json:"rpm_override"`
}

// UserGroupRPMOverrideRepository stores private member RPM policies. It is
// deliberately independent of pricing and works with the Personal SQLite
// runtime.
type UserGroupRPMOverrideRepository interface {
	GetRPMOverrideByUserAndGroup(ctx context.Context, userID, groupID int64) (*int, error)
	ListByGroupID(ctx context.Context, groupID int64) ([]UserGroupRPMOverrideEntry, error)
	SyncGroupRPMOverrides(ctx context.Context, groupID int64, entries []GroupRPMOverrideInput) error
	ClearGroupRPMOverrides(ctx context.Context, groupID int64) error
	DeleteByGroupID(ctx context.Context, groupID int64) error
	DeleteByUserID(ctx context.Context, userID int64) error
}
