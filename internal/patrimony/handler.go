package patrimony

import (
	"Sobra_Ai_Back-end/internal/database"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func RegisterRoutes(rg *gin.RouterGroup) {
	r := rg.Group("/patrimony")
	r.GET("/", getPatrimony)
	r.PUT("/fgts", putFGTS)
	r.DELETE("/fgts", deleteFGTS)
	r.POST("/investments", createInvestment)
	r.PATCH("/investments/:id", updateInvestment)
	r.DELETE("/investments/:id", deleteInvestment)
	r.GET("/investment-categories", listInvestmentCategories)
	r.POST("/investment-categories", createInvestmentCategory)
	r.PATCH("/investment-categories/:id", updateInvestmentCategory)
	r.DELETE("/investment-categories/:id", deleteInvestmentCategory)
}

func currentUser(c *gin.Context) (uint, bool) {
	value, _ := c.Get("user_id")
	id, ok := value.(uint)
	if !ok || id == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return 0, false
	}
	return id, true
}

func decode(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		badRequest(c)
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		badRequest(c)
		return false
	}
	return true
}

func badRequest(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "Dados invalidos: informe os campos permitidos e um valor entre 0 e 9999999999.99 com ate duas casas decimais"})
}

// Parse the JSON number exactly, avoiding binary rounding of currency input.
type amountNumber string

func (n *amountNumber) UnmarshalJSON(raw []byte) error {
	if !amountPattern.Match(raw) {
		return errors.New("invalid monetary amount")
	}
	*n = amountNumber(raw)
	return nil
}

var amountPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$`)

func parseAmount(number *amountNumber) (int64, bool) {
	if number == nil || !amountPattern.MatchString(string(*number)) {
		return 0, false
	}
	value, ok := new(big.Rat).SetString(string(*number))
	if !ok || value.Sign() < 0 {
		return 0, false
	}
	value.Mul(value, big.NewRat(100, 1))
	if !value.IsInt() || !value.Num().IsInt64() {
		return 0, false
	}
	cents := value.Num().Int64()
	return cents, cents <= 999999999999
}

func textValid(value string, max int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= max
}

func getPatrimony(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	var fgts FGTSBalance
	items := []Investment{}
	err := database.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).First(&fgts).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Where("user_id = ?", userID).Order("id asc").Find(&items).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		serverError(c)
		return
	}
	var fgtsData any
	if fgts.UserID != 0 {
		fgtsData = gin.H{"amount": float64(fgts.AmountCents) / 100, "updated_at": fgts.UpdatedAt}
	}
	data := make([]investmentResponse, 0, len(items))
	var total int64
	for _, item := range items {
		// Keep the JSON monetary totals within the exact integer range of float64.
		if item.AmountCents > (1<<53)-1-total-fgts.AmountCents {
			serverError(c)
			return
		}
		total += item.AmountCents
		data = append(data, investmentData(item))
	}
	c.JSON(http.StatusOK, gin.H{"fgts": fgtsData, "investments": data, "total_fgts": float64(fgts.AmountCents) / 100, "total_investments": float64(total) / 100, "total_patrimony": float64(total+fgts.AmountCents) / 100})
}

func putFGTS(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	var req struct {
		Amount *amountNumber `json:"amount"`
	}
	if !decode(c, &req) {
		return
	}
	cents, ok := parseAmount(req.Amount)
	if !ok {
		badRequest(c)
		return
	}
	item := FGTSBalance{UserID: userID, AmountCents: cents, UpdatedAt: time.Now().UTC()}
	err := database.DB.WithContext(c.Request.Context()).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"amount_cents", "updated_at"})}, clause.Returning{}).Create(&item).Error
	if err != nil {
		serverError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"amount": float64(item.AmountCents) / 100, "updated_at": item.UpdatedAt}})
}

func deleteFGTS(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	result := database.DB.WithContext(c.Request.Context()).Where("user_id = ?", userID).Delete(&FGTSBalance{})
	deleted(c, result)
}

type investmentRequest struct {
	Name       *string       `json:"name"`
	Category   *string       `json:"category"`
	CategoryID *uint64       `json:"category_id"`
	Amount     *amountNumber `json:"amount"`
}

func createInvestment(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	var req investmentRequest
	if !decode(c, &req) {
		return
	}
	cents, valid := parseAmount(req.Amount)
	if !valid || req.Name == nil || !textValid(*req.Name, 120) || (req.Category == nil) == (req.CategoryID == nil) || (req.Category != nil && !validCategoryName(*req.Category)) || (req.CategoryID != nil && (*req.CategoryID == 0 || *req.CategoryID > math.MaxInt64)) {
		badRequest(c)
		return
	}
	item := Investment{UserID: userID, Name: strings.TrimSpace(*req.Name), AmountCents: cents}
	if !categoryWrite(c, userID, func(tx *gorm.DB) error {
		category, err := resolveCategory(tx, userID, req.CategoryID, req.Category)
		if err != nil {
			return err
		}
		item.CategoryID, item.Category = category.ID, category.Name
		return tx.Create(&item).Error
	}) {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": investmentData(item)})
}

func investmentID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 63)
	if err != nil || id == 0 {
		badRequest(c)
		return 0, false
	}
	return id, true
}

func updateInvestment(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := investmentID(c)
	if !ok {
		return
	}
	// Reject explicit nulls as well as unknown fields, rather than silently ignoring them.
	var fields map[string]json.RawMessage
	if !decode(c, &fields) {
		return
	}
	if len(fields) == 0 {
		badRequest(c)
		return
	}
	updates := map[string]any{}
	var categoryName *string
	var categoryID *uint64
	for key, raw := range fields {
		switch key {
		case "name", "category":
			var value *string
			max := 120
			if key == "category" {
				max = 80
			}
			if json.Unmarshal(raw, &value) != nil || value == nil || !textValid(*value, max) {
				badRequest(c)
				return
			}
			if key == "category" {
				if !validCategoryName(*value) {
					badRequest(c)
					return
				}
				categoryName = value
			} else {
				updates[key] = strings.TrimSpace(*value)
			}
		case "category_id":
			if json.Unmarshal(raw, &categoryID) != nil || categoryID == nil || *categoryID == 0 || *categoryID > math.MaxInt64 {
				badRequest(c)
				return
			}
		case "amount":
			var value *amountNumber
			if json.Unmarshal(raw, &value) != nil {
				badRequest(c)
				return
			}
			cents, valid := parseAmount(value)
			if !valid {
				badRequest(c)
				return
			}
			updates["amount_cents"] = cents
		default:
			badRequest(c)
			return
		}
	}
	if categoryID != nil && categoryName != nil {
		badRequest(c)
		return
	}
	var item Investment
	if !categoryWrite(c, userID, func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND user_id = ?", id, userID).First(&item).Error; err != nil {
			return err
		}
		if categoryID != nil || categoryName != nil {
			category, err := resolveCategory(tx, userID, categoryID, categoryName)
			if err != nil {
				return err
			}
			updates["category_id"], updates["category"] = category.ID, category.Name
		}
		return tx.Model(&item).Clauses(clause.Returning{}).Updates(updates).Error
	}) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": investmentData(item)})
}

func deleteInvestment(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := investmentID(c)
	if !ok {
		return
	}
	deleted(c, database.DB.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", id, userID).Delete(&Investment{}))
}

func deleted(c *gin.Context, result *gorm.DB) {
	if result.Error != nil {
		serverError(c)
		return
	}
	if result.RowsAffected == 0 {
		notFound(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Registro removido com sucesso"})
}

func notFound(c *gin.Context) { c.JSON(http.StatusNotFound, gin.H{"error": "Registro nao encontrado"}) }
func serverError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao acessar patrimonio"})
}
