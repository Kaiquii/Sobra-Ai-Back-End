package patrimony

import (
	"Sobra_Ai_Back-end/internal/database"
	"errors"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InvestmentCategory struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	UserID *uint  `json:"-"`
	Name   string `json:"name"`
}

type categoryResponse struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

func categoryData(category InvestmentCategory) categoryResponse {
	return categoryResponse{ID: category.ID, Name: category.Name, IsDefault: category.UserID == nil}
}

var (
	errCategoryInput   = errors.New("invalid category")
	errDefaultCategory = errors.New("default category is immutable")
	errCategoryExists  = errors.New("category already exists")
	errCategoryUsed    = errors.New("category is in use")
)

func validCategoryName(name string) bool {
	return utf8.ValidString(name) && !strings.ContainsRune(name, 0) && textValid(name, 80)
}

// Serialize a user's category and investment writes so rename/delete cannot race
// with a new association, and legacy text and category_id remain consistent.
func categoryWrite(c *gin.Context, userID uint, work func(*gorm.DB) error) bool {
	err := database.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var user struct{ ID uint }
		if err := tx.Table("users").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).Take(&user).Error; err != nil {
			return err
		}
		return work(tx)
	})
	if err == nil {
		return true
	}
	var pgError *pgconn.PgError
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		notFound(c)
	case errors.Is(err, errCategoryInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": "Informe uma categoria valida: category_id positivo ou nome com 1 a 80 caracteres", "code": "invalid_category"})
	case errors.Is(err, errDefaultCategory):
		c.JSON(http.StatusForbidden, gin.H{"error": "As categorias padrao nao podem ser alteradas ou excluidas", "code": "default_category"})
	case errors.Is(err, errCategoryUsed):
		c.JSON(http.StatusConflict, gin.H{"error": "Troque a categoria dos investimentos vinculados antes de excluir", "code": "category_in_use"})
	case errors.Is(err, errCategoryExists), errors.As(err, &pgError) && pgError.Code == "23505":
		c.JSON(http.StatusConflict, gin.H{"error": "Ja existe uma categoria com esse nome", "code": "category_exists"})
	default:
		serverError(c)
	}
	return false
}

func visibleCategories(tx *gorm.DB, userID uint) *gorm.DB {
	return tx.Where("user_id IS NULL OR user_id = ?", userID)
}

func resolveCategory(tx *gorm.DB, userID uint, id *uint64, name *string) (InvestmentCategory, error) {
	var category InvestmentCategory
	if (id == nil) == (name == nil) {
		return category, errCategoryInput
	}
	if id != nil {
		if *id == 0 || *id > math.MaxInt64 {
			return category, errCategoryInput
		}
		err := visibleCategories(tx, userID).Where("id = ?", *id).First(&category).Error
		return category, err
	}
	if !validCategoryName(*name) {
		return category, errCategoryInput
	}
	label := strings.TrimSpace(*name)
	err := visibleCategories(tx, userID).Where("lower(trim(name)) = lower(?)", label).Order("user_id NULLS FIRST, id").First(&category).Error
	if err == nil {
		return category, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return category, err
	}
	category = InvestmentCategory{UserID: &userID, Name: label}
	err = tx.Create(&category).Error
	return category, err
}

func listInvestmentCategories(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	var categories []InvestmentCategory
	if err := visibleCategories(database.DB.WithContext(c.Request.Context()), userID).Order("user_id NULLS FIRST, lower(name), id").Find(&categories).Error; err != nil {
		serverError(c)
		return
	}
	data := make([]categoryResponse, 0, len(categories))
	for _, category := range categories {
		data = append(data, categoryData(category))
	}
	c.JSON(http.StatusOK, gin.H{"categories": data, "total": len(data)})
}

func categoryNameRequest(c *gin.Context) (string, bool) {
	var request struct {
		Name *string `json:"name"`
	}
	if !decode(c, &request) {
		return "", false
	}
	if request.Name == nil || !validCategoryName(*request.Name) {
		badRequest(c)
		return "", false
	}
	return strings.TrimSpace(*request.Name), true
}

func categoryNameAvailable(tx *gorm.DB, userID uint, name string, except uint) error {
	var count int64
	if err := visibleCategories(tx.Model(&InvestmentCategory{}), userID).Where("lower(trim(name)) = lower(?) AND id <> ?", name, except).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errCategoryExists
	}
	return nil
}

func createInvestmentCategory(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	name, ok := categoryNameRequest(c)
	if !ok {
		return
	}
	category := InvestmentCategory{UserID: &userID, Name: name}
	if !categoryWrite(c, userID, func(tx *gorm.DB) error {
		if err := categoryNameAvailable(tx, userID, name, 0); err != nil {
			return err
		}
		return tx.Create(&category).Error
	}) {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": categoryData(category)})
}

func editableCategory(tx *gorm.DB, userID uint, id uint64) (InvestmentCategory, error) {
	var category InvestmentCategory
	if err := visibleCategories(tx, userID).Where("id = ?", id).First(&category).Error; err != nil {
		return category, err
	}
	if category.UserID == nil {
		return category, errDefaultCategory
	}
	return category, nil
}

func updateInvestmentCategory(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := investmentID(c)
	if !ok {
		return
	}
	name, ok := categoryNameRequest(c)
	if !ok {
		return
	}
	var category InvestmentCategory
	if !categoryWrite(c, userID, func(tx *gorm.DB) error {
		var err error
		category, err = editableCategory(tx, userID, id)
		if err != nil {
			return err
		}
		if err = categoryNameAvailable(tx, userID, name, category.ID); err != nil {
			return err
		}
		if err = tx.Model(&category).Update("name", name).Error; err != nil {
			return err
		}
		// A single transaction updates the legacy label on every linked investment.
		return tx.Model(&Investment{}).Where("user_id = ? AND category_id = ?", userID, id).UpdateColumn("category", name).Error
	}) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": categoryData(category)})
}

func deleteInvestmentCategory(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := investmentID(c)
	if !ok {
		return
	}
	if !categoryWrite(c, userID, func(tx *gorm.DB) error {
		category, err := editableCategory(tx, userID, id)
		if err != nil {
			return err
		}
		var count int64
		if err = tx.Model(&Investment{}).Where("user_id = ? AND category_id = ?", userID, id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errCategoryUsed
		}
		return tx.Delete(&category).Error
	}) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Categoria excluida com sucesso"})
}
