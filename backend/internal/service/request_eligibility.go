package service

import "context"

// RequestEligibilityChecker is the gateway admission boundary. Personal
// Edition supplies an implementation with no commercial dependencies.
type RequestEligibilityChecker interface {
	CheckBillingEligibility(context.Context, *User, *APIKey, *Group, *UserSubscription, string) error
}

type PersonalRequestEligibilityChecker struct{}

func (PersonalRequestEligibilityChecker) CheckBillingEligibility(context.Context, *User, *APIKey, *Group, *UserSubscription, string) error {
	return nil
}

func ProvidePersonalRequestEligibilityChecker() RequestEligibilityChecker {
	return PersonalRequestEligibilityChecker{}
}

func ProvideBillingRequestEligibilityChecker(service *BillingCacheService) RequestEligibilityChecker {
	return service
}
