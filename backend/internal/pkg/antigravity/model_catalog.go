package antigravity

import "time"

const ModelAvailabilityExtraKey = "antigravity_model_availability"

type ModelAvailability string

const (
	ModelAvailabilityRecommended ModelAvailability = "recommended"
	ModelAvailabilityVerified    ModelAvailability = "verified"
	ModelAvailabilityUnavailable ModelAvailability = "currently_unavailable"
)

type ModelCatalogEntry struct {
	ID           string            `json:"id"`
	DisplayName  string            `json:"display_name"`
	Availability ModelAvailability `json:"availability"`
	Recommended  bool              `json:"recommended"`
}

type ModelAvailabilitySnapshot struct {
	Models    []string  `json:"models"`
	CheckedAt time.Time `json:"checked_at"`
	Source    string    `json:"source"`
}

var recommendedModelIDs = []string{
	"gemini-3.1-pro-high",
	"gemini-3.6-flash-high",
	"gemini-3.6-flash-medium",
	"gemini-3.6-flash-low",
	"gemini-3.1-flash-image",
	"claude-sonnet-4-6",
}

var advancedVerifiedModelIDs = []string{
	"claude-opus-4-5-thinking",
	"claude-opus-4-6",
	"claude-opus-4-6-thinking",
	"gemini-2.5-flash",
	"gemini-2.5-flash-lite",
	"gemini-2.5-flash-thinking",
	"gemini-3-flash",
	"gemini-3-pro-low",
	"gemini-3-pro-high",
	"gemini-3.1-pro-low",
	"gemini-3.1-flash-image-preview",
	"gemini-3.6-flash-tiered",
	"gemini-3-pro-preview",
	"gemini-3-pro-image",
}

var currentlyUnavailableModelIDs = []string{
	"claude-fable-5",
	"claude-sonnet-4-5",
	"claude-sonnet-4-5-thinking",
	"claude-opus-4-7",
	"claude-opus-4-8",
	"gemini-2.5-flash-image",
	"gemini-2.5-flash-image-preview",
	"gemini-3.6-flash",
}

func RecommendedModelIDs() []string { return append([]string(nil), recommendedModelIDs...) }

func AdvancedVerifiedModelIDs() []string {
	return append([]string(nil), advancedVerifiedModelIDs...)
}

func VerifiedModelIDs() []string {
	result := RecommendedModelIDs()
	return append(result, advancedVerifiedModelIDs...)
}

func CurrentlyUnavailableModelIDs() []string {
	return append([]string(nil), currentlyUnavailableModelIDs...)
}

func modelDisplayName(id string) string {
	for _, model := range append(append([]modelDef(nil), claudeModels...), geminiModels...) {
		if model.ID == id {
			return model.DisplayName
		}
	}
	return id
}

func PersonalModelCatalog() []ModelCatalogEntry {
	result := make([]ModelCatalogEntry, 0, len(recommendedModelIDs)+len(advancedVerifiedModelIDs)+len(currentlyUnavailableModelIDs))
	for _, id := range recommendedModelIDs {
		result = append(result, ModelCatalogEntry{ID: id, DisplayName: modelDisplayName(id), Availability: ModelAvailabilityRecommended, Recommended: true})
	}
	for _, id := range advancedVerifiedModelIDs {
		result = append(result, ModelCatalogEntry{ID: id, DisplayName: modelDisplayName(id), Availability: ModelAvailabilityVerified})
	}
	for _, id := range currentlyUnavailableModelIDs {
		result = append(result, ModelCatalogEntry{ID: id, DisplayName: modelDisplayName(id), Availability: ModelAvailabilityUnavailable})
	}
	return result
}

// VerifiedModelsForAccountTest deliberately excludes compatibility aliases and
// models observed as unavailable. Compatibility remains a separate mapping concern.
func VerifiedModelsForAccountTest() []ClaudeModel {
	ids := VerifiedModelIDs()
	result := make([]ClaudeModel, 0, len(ids))
	for _, id := range ids {
		result = append(result, ClaudeModel{
			ID:           id,
			Type:         "model",
			DisplayName:  modelDisplayName(id),
			CreatedAt:    "",
			Availability: string(ModelAvailabilityVerified),
			Recommended:  containsModelID(recommendedModelIDs, id),
		})
	}
	return result
}

func containsModelID(models []string, id string) bool {
	for _, model := range models {
		if model == id {
			return true
		}
	}
	return false
}
