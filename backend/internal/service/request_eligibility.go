package service

import "context"

var (
	ErrGroupRPMExceeded = ErrAPIKeyRateLimited.WithMetadata(map[string]string{"scope": "group"})
	ErrUserRPMExceeded  = ErrAPIKeyRateLimited.WithMetadata(map[string]string{"scope": "user"})
)

// RequestEligibilityChecker is the gateway admission boundary. Personal
// Edition supplies an implementation with no commercial dependencies.
type RequestEligibilityChecker interface {
	CheckRequestEligibility(context.Context, *User, *APIKey, *Group, any, string) error
}

type PersonalRequestEligibilityChecker struct{}

func (PersonalRequestEligibilityChecker) CheckRequestEligibility(context.Context, *User, *APIKey, *Group, any, string) error {
	return nil
}

func ProvidePersonalRequestEligibilityChecker() RequestEligibilityChecker {
	return PersonalRequestEligibilityChecker{}
}
