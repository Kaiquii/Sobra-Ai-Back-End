package users

import (
	"Sobra_Ai_Back-end/internal/auth"
	"Sobra_Ai_Back-end/internal/database"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Balance preferences affect the available-balance presentation, never the
// recorded income, expense or payment-source amounts.
type BalancePreferences struct {
	UserID                 uint      `gorm:"primaryKey" json:"-"`
	ExcludeVAFromAvailable bool      `gorm:"not null;default:false" json:"exclude_va_from_available"`
	ExcludeVRFromAvailable bool      `gorm:"not null;default:false" json:"exclude_vr_from_available"`
	UpdatedAt              time.Time `gorm:"not null;autoUpdateTime" json:"-"`
}

func (BalancePreferences) TableName() string { return "user_balance_preferences" }

// A missing preferences row means the original behavior. Reads never create a
// row, so existing users keep both options disabled without a data backfill.
func LoadBalancePreferences(ctx context.Context, db *gorm.DB, userID uint) (BalancePreferences, error) {
	preferences := BalancePreferences{UserID: userID}
	if db == nil || userID == 0 {
		return preferences, errors.New("preferencias de saldo indisponiveis")
	}
	err := db.WithContext(ctx).Where("user_id = ?", userID).Take(&preferences).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return BalancePreferences{UserID: userID}, nil
	}
	return preferences, err
}

type balancePreferencesPatch struct {
	excludeVA *bool
	excludeVR *bool
}

func decodeBalancePreferencesPatch(reader io.Reader) (balancePreferencesPatch, error) {
	var patch balancePreferencesPatch
	invalid := errors.New("preferencias de saldo invalidas")
	decoder := json.NewDecoder(reader)
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return patch, invalid
	}
	seen := make(map[string]bool, 2)
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok || seen[key] || (key != "exclude_va_from_available" && key != "exclude_vr_from_available") {
			return patch, invalid
		}
		seen[key] = true
		valueToken, err := decoder.Token()
		value, ok := valueToken.(bool)
		if err != nil || !ok {
			return patch, invalid
		}
		switch key {
		case "exclude_va_from_available":
			patch.excludeVA = &value
		case "exclude_vr_from_available":
			patch.excludeVR = &value
		}
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return patch, invalid
	}
	return patch, nil
}

func balancePreferencesUser(c *gin.Context) (uint, bool) {
	userID, ok := currentUserID(c)
	if !ok || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return 0, false
	}
	return userID, true
}

func balancePreferencesUserExists(c *gin.Context, userID uint) bool {
	if database.DB == nil {
		balancePreferencesFailure(c)
		return false
	}
	var user auth.User
	err := database.DB.WithContext(c.Request.Context()).Select("id").First(&user, userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Usuario nao encontrado"})
		return false
	}
	if err != nil {
		balancePreferencesFailure(c)
		return false
	}
	return true
}

func getBalancePreferences(c *gin.Context) {
	userID, ok := balancePreferencesUser(c)
	if !ok || !balancePreferencesUserExists(c, userID) {
		return
	}
	preferences, err := LoadBalancePreferences(c.Request.Context(), database.DB, userID)
	if err != nil {
		balancePreferencesFailure(c)
		return
	}
	c.JSON(http.StatusOK, preferences)
}

func updateBalancePreferences(c *gin.Context) {
	userID, ok := balancePreferencesUser(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	patch, err := decodeBalancePreferencesPatch(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Informe somente exclude_va_from_available e exclude_vr_from_available como booleanos", "code": "invalid_balance_preferences"})
		return
	}
	if !balancePreferencesUserExists(c, userID) {
		return
	}
	if patch.excludeVA == nil && patch.excludeVR == nil {
		preferences, err := LoadBalancePreferences(c.Request.Context(), database.DB, userID)
		if err != nil {
			balancePreferencesFailure(c)
			return
		}
		c.JSON(http.StatusOK, preferences)
		return
	}
	preferences := BalancePreferences{UserID: userID, UpdatedAt: time.Now()}
	columns := []string{"updated_at"}
	if patch.excludeVA != nil {
		preferences.ExcludeVAFromAvailable = *patch.excludeVA
		columns = append(columns, "exclude_va_from_available")
	}
	if patch.excludeVR != nil {
		preferences.ExcludeVRFromAvailable = *patch.excludeVR
		columns = append(columns, "exclude_vr_from_available")
	}
	// Only present flags participate in ON CONFLICT. Concurrent updates to
	// different flags preserve each other, including on the first insert.
	err = database.DB.WithContext(c.Request.Context()).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns(columns),
	}, clause.Returning{}).Create(&preferences).Error
	if err != nil {
		balancePreferencesFailure(c)
		return
	}
	c.JSON(http.StatusOK, preferences)
}

func balancePreferencesFailure(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Nao foi possivel acessar as preferencias de saldo"})
}
