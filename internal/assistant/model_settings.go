package assistant

import (
	"Sobra_Ai_Back-end/internal/database"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A blank ModelID means the server recommendation. User preferences never contain
// API credentials or grant access to a model outside the verified free catalog.
type assistantModelPreference struct {
	UserID    uint      `gorm:"primaryKey"`
	ModelID   string    `gorm:"size:160;not null"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime"`
}

func (assistantModelPreference) TableName() string { return "assistant_model_preferences" }

type assistantModelPreferenceResponse struct {
	ModelID          string `json:"model_id"`
	EffectiveModelID string `json:"effective_model_id"`
	Available        bool   `json:"available"`
}

func assistantSettingsUser(c *gin.Context) (uint, bool) {
	value, _ := c.Get("user_id")
	id, ok := value.(uint)
	if !ok || id == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return 0, false
	}
	return id, true
}

func preferredAssistantModel(userID uint) (string, error) {
	if database.DB == nil {
		return "", errors.New("banco indisponivel")
	}
	var preference assistantModelPreference
	err := database.DB.Where("user_id = ?", userID).First(&preference).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return preference.ModelID, err
}

func settingsEffectiveModel(preference string, models []AssistantModel) assistantModelPreferenceResponse {
	response := assistantModelPreferenceResponse{ModelID: preference}
	if len(models) == 0 {
		return response
	}
	response.EffectiveModelID = models[0].ID
	for _, model := range models {
		if model.Recommended {
			response.EffectiveModelID = model.ID
			break
		}
	}
	if preference == "" {
		response.Available = true
		return response
	}
	for _, model := range models {
		if model.ID == preference {
			response.Available = true
			response.EffectiveModelID = model.ID
			break
		}
	}
	return response
}

func listAssistantModels(c *gin.Context) {
	userID, ok := assistantSettingsUser(c)
	if !ok {
		return
	}
	models, err := listFreeAssistantModels(c.Request.Context())
	if err != nil || len(models) == 0 {
		assistantModelsUnavailable(c)
		return
	}
	preference, err := preferredAssistantModel(userID)
	if err != nil {
		assistantPreferenceFailure(c)
		return
	}
	selection := settingsEffectiveModel(preference, models)
	selected := preference
	if !selection.Available {
		selected = ""
	}
	recommendation := settingsEffectiveModel("", models)
	c.JSON(http.StatusOK, gin.H{
		"models":             models,
		"selected_model":     selected,
		"recommended_model":  recommendation.EffectiveModelID,
		"effective_model_id": selection.EffectiveModelID,
		"automatic_fallback": true,
	})
}

func getAssistantModelPreference(c *gin.Context) {
	userID, ok := assistantSettingsUser(c)
	if !ok {
		return
	}
	models, err := listFreeAssistantModels(c.Request.Context())
	if err != nil || len(models) == 0 {
		assistantModelsUnavailable(c)
		return
	}
	preference, err := preferredAssistantModel(userID)
	if err != nil {
		assistantPreferenceFailure(c)
		return
	}
	c.JSON(http.StatusOK, settingsEffectiveModel(preference, models))
}

func decodeModelPreference(c *gin.Context) (string, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var request struct {
		ModelID *string `json:"model_id"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.ModelID == nil {
		return "", false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", false
	}
	modelID := strings.TrimSpace(*request.ModelID)
	if !utf8.ValidString(modelID) || len(modelID) > 160 {
		return "", false
	}
	return modelID, true
}

func updateAssistantModelPreference(c *gin.Context) {
	userID, ok := assistantSettingsUser(c)
	if !ok {
		return
	}
	modelID, ok := decodeModelPreference(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Informe somente model_id; use uma string vazia para o recomendado", "code": "invalid_assistant_model"})
		return
	}
	models, err := listFreeAssistantModels(c.Request.Context())
	if err != nil || len(models) == 0 {
		assistantModelsUnavailable(c)
		return
	}
	selection := settingsEffectiveModel(modelID, models)
	if !selection.Available {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Escolha um modelo gratuito disponivel no catalogo", "code": "invalid_assistant_model"})
		return
	}
	if database.DB == nil {
		assistantPreferenceFailure(c)
		return
	}
	preference := assistantModelPreference{UserID: userID, ModelID: modelID, UpdatedAt: time.Now()}
	err = database.DB.WithContext(c.Request.Context()).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"model_id", "updated_at"}),
	}).Create(&preference).Error
	if err != nil {
		assistantPreferenceFailure(c)
		return
	}
	c.JSON(http.StatusOK, selection)
}

func assistantModelsUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Catalogo de modelos gratuitos temporariamente indisponivel", "code": "assistant_models_unavailable"})
}

func assistantPreferenceFailure(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Nao foi possivel acessar a preferencia de modelo"})
}
