package service

import (
	"errors"
	"strings"
)

type antigravityAuthFailureClass string

const (
	antigravityAuthFailureTransient           antigravityAuthFailureClass = "transient_failure"
	antigravityAuthFailureAccessTokenRejected antigravityAuthFailureClass = "access_token_rejected"
	antigravityAuthFailureReauthRequired      antigravityAuthFailureClass = "reauth_required"
	antigravityAuthFailureProviderConfig      antigravityAuthFailureClass = "provider_config_error"
	antigravityAuthFailurePolicyBlocked       antigravityAuthFailureClass = "policy_blocked"
)

// antigravityAuthFailure is emitted only after OAuthRefreshAPI has re-read the
// durable account and exhausted refresh-race recovery. Consumers must not infer
// reauthorization from a raw business-API 401.
type antigravityAuthFailure struct {
	class  antigravityAuthFailureClass
	reason string
	err    error
}

func (e *antigravityAuthFailure) Error() string {
	if e == nil || e.err == nil {
		return "antigravity authentication failure"
	}
	return e.err.Error()
}

func (e *antigravityAuthFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func classifyFinalAntigravityRefreshError(err error) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	failure := &antigravityAuthFailure{class: antigravityAuthFailureTransient, reason: "refresh_failed", err: err}
	switch {
	case strings.Contains(lower, "invalid_rapt"):
		failure.class = antigravityAuthFailureReauthRequired
		failure.reason = "google_reauthentication_required"
	case strings.Contains(lower, "invalid_grant") ||
		strings.Contains(lower, "refresh token revoked") ||
		strings.Contains(lower, "refresh_token revoked"):
		failure.class = antigravityAuthFailureReauthRequired
		failure.reason = "refresh_credential_invalid"
	case isMissingAntigravityRefreshTokenMessage(lower):
		failure.class = antigravityAuthFailureReauthRequired
		failure.reason = "refresh_token_missing"
	case strings.Contains(lower, "invalid_client") || strings.Contains(lower, "unauthorized_client") ||
		strings.Contains(lower, "invalid_scope") || strings.Contains(lower, "unknown scope"):
		failure.class = antigravityAuthFailureProviderConfig
		failure.reason = "oauth_client_configuration"
	case strings.Contains(lower, "admin_policy_enforced") || strings.Contains(lower, "access_denied"):
		failure.class = antigravityAuthFailurePolicyBlocked
		failure.reason = "google_account_policy"
	case strings.Contains(lower, "http 401") || strings.Contains(lower, "unauthenticated"):
		failure.class = antigravityAuthFailureAccessTokenRejected
		failure.reason = "access_token_rejected"
	}
	return failure
}

func isMissingAntigravityRefreshTokenMessage(lower string) bool {
	return strings.Contains(lower, "missing refresh_token") ||
		strings.Contains(lower, "refresh token missing") ||
		strings.Contains(lower, "refresh token not found") ||
		strings.Contains(lower, "refresh_token not found") ||
		strings.Contains(lower, "no refresh token")
}

func antigravityFailureClass(err error) (antigravityAuthFailureClass, bool) {
	var failure *antigravityAuthFailure
	if !errors.As(err, &failure) || failure == nil {
		return "", false
	}
	return failure.class, true
}
