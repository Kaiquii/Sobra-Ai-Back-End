package incomes

import (
	"Sobra_Ai_Back-end/internal/database"
	"gorm.io/gorm"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(rg *gin.RouterGroup) {
	incomesGroup := rg.Group("/incomes")
	{
		incomesGroup.POST("/", createIncome)
		incomesGroup.GET("/", getIncomes)
		incomesGroup.PATCH("/:id", updateIncome)
		incomesGroup.DELETE("/:id", deleteIncome)
	}
}

func getUserID(c *gin.Context) (uint, bool) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	return userIDObj.(uint), true
}

func createIncome(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}

	var req CreateIncomeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos: " + err.Error()})
		return
	}

	source := normalizeIncomeSource(req.Source)
	if source == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Fonte inválida. Use Salário, Adiantamento, Renda Extra, VA ou VR."})
		return
	}

	incomeType := normalizeIncomeType(req.Type)
	if incomeType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tipo inválido. Use Única ou Fixa."})
		return
	}

	loopCount := 1

	isRendaExtra := source == "Renda Extra"
	isFixedIncome := incomeType == "Fixa"

	if isFixedIncome {
		if isRendaExtra {
			if req.RepeatFuture {
				loopCount = 60
			}
		} else {
			loopCount = 60
		}
	}

	currentMonth := req.Month
	currentYear := req.Year

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		for i := 0; i < loopCount; i++ {
			newIncome := Income{
				UserID: userID,
				Source: source,
				Amount: req.Amount,
				Month:  currentMonth,
				Year:   currentYear,
			}

			if err := tx.Create(&newIncome).Error; err != nil {
				return err
			}

			currentMonth++
			if currentMonth > 12 {
				currentMonth = 1
				currentYear++
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar renda"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Renda(s) cadastrada(s) com sucesso!"})
}

func getIncomes(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}

	var incomesList []Income
	query := database.DB.Where("user_id = ?", userID)

	monthStr := c.Query("month")
	yearStr := c.Query("year")

	if monthStr != "" {
		if month, err := strconv.Atoi(monthStr); err == nil {
			query = query.Where("month = ?", month)
		}
	}
	if yearStr != "" {
		if year, err := strconv.Atoi(yearStr); err == nil {
			query = query.Where("year = ?", year)
		}
	}

	if err := query.Find(&incomesList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar rendas"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"total": len(incomesList), "incomes": incomesList})
}

func updateIncome(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}

	id := c.Param("id")
	var income Income

	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).First(&income).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Renda não encontrada ou não pertence a você"})
		return
	}

	var updateData map[string]interface{}
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos: " + err.Error()})
		return
	}

	updateFuture := false
	if val, exists := updateData["update_future"]; exists {
		boolVal, valid := val.(bool)
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{"error": "update_future deve ser booleano"})
			return
		}
		updateFuture = boolVal
		delete(updateData, "update_future")
	}

	for key, value := range updateData {
		switch key {
		case "amount":
			amount, valid := value.(float64)
			if !valid || amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Valor da renda deve ser maior que zero"})
				return
			}
		case "source":
			text, valid := value.(string)
			source := normalizeIncomeSource(text)
			if !valid || source == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Fonte inválida"})
				return
			}
			updateData[key] = source
		case "month", "year":
			number, valid := value.(float64)
			if !valid || number != math.Trunc(number) || (key == "month" && (number < 1 || number > 12)) || (key == "year" && number < 2000) || updateFuture {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Período inválido; não altere month/year com update_future"})
				return
			}
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Campo de renda não permitido: " + key})
			return
		}
	}
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&Income{}).Where("user_id = ?", userID)
		if updateFuture {
			query = query.Where("id = ? OR (source = ? AND (year > ? OR (year = ? AND month > ?)))", income.ID, income.Source, income.Year, income.Year, income.Month)
		} else {
			query = query.Where("id = ?", income.ID)
		}
		return query.Updates(updateData).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao atualizar renda"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Renda(s) atualizada(s) com sucesso!"})
}

func deleteIncome(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}

	id := c.Param("id")
	deleteFuture := c.Query("delete_future") == "true"

	var income Income
	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).First(&income).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Renda não encontrada ou não pertence a você"})
		return
	}

	if deleteFuture {
		err := database.DB.Where(
			"user_id = ? AND source = ? AND (year > ? OR (year = ? AND month >= ?))",
			userID, income.Source, income.Year, income.Year, income.Month,
		).Delete(&Income{}).Error

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao deletar renda atual e futuras"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Renda atual e futuras deletadas com sucesso!"})
		return
	}

	if err := database.DB.Delete(&income).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao deletar renda"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Renda deletada com sucesso!"})
}

func normalizeIncomeSource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "salário", "salario":
		return "Salário"
	case "adiantamento":
		return "Adiantamento"
	case "renda extra", "renda_extra":
		return "Renda Extra"
	case "va", "vale alimentação", "vale alimentacao", "vale-alimentação", "vale-alimentacao", "vale_alimentacao":
		return "VA"
	case "vr", "vale refeição", "vale refeicao", "vale-refeição", "vale-refeicao", "vale_refeicao":
		return "VR"
	default:
		return ""
	}
}

func normalizeIncomeType(incomeType string) string {
	switch strings.TrimSpace(strings.ToLower(incomeType)) {
	case "", "única", "unica":
		return "Única"
	case "fixa":
		return "Fixa"
	default:
		return ""
	}
}
