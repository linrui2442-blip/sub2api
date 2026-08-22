package geminicli

import (
	"bytes"
	"encoding/json"
)

// LoadCodeAssistRequest matches done-hub's internal Code Assist call.
type LoadCodeAssistRequest struct {
	CloudAICompanionProject string                 `json:"cloudaicompanionProject,omitempty"`
	Metadata                LoadCodeAssistMetadata `json:"metadata"`
	Mode                    string                 `json:"mode,omitempty"`
}

type LoadCodeAssistMetadata struct {
	IDEType     string `json:"ideType"`
	Platform    string `json:"platform"`
	PluginType  string `json:"pluginType"`
	DuetProject string `json:"duetProject,omitempty"`
}

type TierInfo struct {
	ID                                 string       `json:"id"`
	Name                               string       `json:"name,omitempty"`
	Description                        string       `json:"description,omitempty"`
	UserDefinedCloudAICompanionProject *bool        `json:"userDefinedCloudaicompanionProject,omitempty"`
	IsDefault                          bool         `json:"isDefault,omitempty"`
	HasAcceptedTOS                     bool         `json:"hasAcceptedTos,omitempty"`
	HasOnboardedPreviously             bool         `json:"hasOnboardedPreviously,omitempty"`
	AvailableCredits                   []CreditInfo `json:"availableCredits,omitempty"`
}

type CreditInfo struct {
	CreditType   string `json:"creditType,omitempty"`
	CreditAmount string `json:"creditAmount,omitempty"`
}

// UnmarshalJSON supports both legacy string tiers and object tiers.
func (t *TierInfo) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var id string
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		t.ID = id
		return nil
	}
	type alias TierInfo
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*t = TierInfo(decoded)
	return nil
}

type LoadCodeAssistResponse struct {
	CurrentTier             *TierInfo        `json:"currentTier,omitempty"`
	PaidTier                *TierInfo        `json:"paidTier,omitempty"`
	CloudAICompanionProject string           `json:"cloudaicompanionProject,omitempty"`
	AllowedTiers            []AllowedTier    `json:"allowedTiers,omitempty"`
	IneligibleTiers         []IneligibleTier `json:"ineligibleTiers,omitempty"`
}

// GetTier extracts tier ID, prioritizing paidTier over currentTier
func (r *LoadCodeAssistResponse) GetTier() string {
	if r.PaidTier != nil && r.PaidTier.ID != "" {
		return r.PaidTier.ID
	}
	if r.CurrentTier != nil {
		return r.CurrentTier.ID
	}
	return ""
}

type AllowedTier struct {
	ID                                 string       `json:"id"`
	Name                               string       `json:"name,omitempty"`
	Description                        string       `json:"description,omitempty"`
	UserDefinedCloudAICompanionProject *bool        `json:"userDefinedCloudaicompanionProject,omitempty"`
	IsDefault                          bool         `json:"isDefault,omitempty"`
	HasAcceptedTOS                     bool         `json:"hasAcceptedTos,omitempty"`
	HasOnboardedPreviously             bool         `json:"hasOnboardedPreviously,omitempty"`
	AvailableCredits                   []CreditInfo `json:"availableCredits,omitempty"`
}

type IneligibleTier struct {
	ReasonCode                  string `json:"reasonCode,omitempty"`
	ReasonMessage               string `json:"reasonMessage,omitempty"`
	TierID                      string `json:"tierId,omitempty"`
	TierName                    string `json:"tierName,omitempty"`
	ValidationErrorMessage      string `json:"validationErrorMessage,omitempty"`
	ValidationURL               string `json:"validationUrl,omitempty"`
	ValidationURLLinkText       string `json:"validationUrlLinkText,omitempty"`
	ValidationLearnMoreURL      string `json:"validationLearnMoreUrl,omitempty"`
	ValidationLearnMoreLinkText string `json:"validationLearnMoreLinkText,omitempty"`
}

type OnboardUserRequest struct {
	TierID                  string                 `json:"tierId"`
	CloudAICompanionProject string                 `json:"cloudaicompanionProject,omitempty"`
	Metadata                LoadCodeAssistMetadata `json:"metadata"`
}

type OnboardUserResponse struct {
	Done     bool                   `json:"done"`
	Response *OnboardUserResultData `json:"response,omitempty"`
	Name     string                 `json:"name,omitempty"`
}

type OnboardUserResultData struct {
	CloudAICompanionProject *CloudAICompanionProject `json:"cloudaicompanionProject,omitempty"`
}

type CloudAICompanionProject struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}
