package service

import "testing"

func TestValidateAntigravityInteractiveOAuthStart(t *testing.T) {
	accountID := int64(42)
	tests := []struct {
		name      string
		reason    AntigravityInteractiveOAuthReason
		accountID *int64
		wantErr   bool
	}{
		{name: "first add", reason: AntigravityOAuthReasonFirstAdd},
		{name: "first add cannot target account", reason: AntigravityOAuthReasonFirstAdd, accountID: &accountID, wantErr: true},
		{name: "confirmed reauth", reason: AntigravityOAuthReasonConfirmedReauth, accountID: &accountID},
		{name: "manual force", reason: AntigravityOAuthReasonManualForce, accountID: &accountID},
		{name: "reauth requires account", reason: AntigravityOAuthReasonConfirmedReauth, wantErr: true},
		{name: "manual force requires account", reason: AntigravityOAuthReasonManualForce, wantErr: true},
		{name: "missing reason", wantErr: true},
		{name: "unknown reason", reason: "automatic_retry", accountID: &accountID, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAntigravityInteractiveOAuthStart(tt.reason, tt.accountID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAntigravityInteractiveOAuthStart() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
