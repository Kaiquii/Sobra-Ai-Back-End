package expenses

import (
	"Sobra_Ai_Back-end/internal/categories"
	"Sobra_Ai_Back-end/internal/database"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const fixedExpenseHorizonMonths = 120
const maxExpenseNotesLength = 500

func normalizeExpenseType(expenseType string) string {
	switch strings.TrimSpace(strings.ToLower(expenseType)) {
	case "única", "unica":
		return "Única"
	case "parcelada":
		return "Parcelada"
	case "fixa":
		return "Fixa"
	default:
		return ""
	}
}

func buildSeriesID(userID uint) string {
	return fmt.Sprintf("expense-%d-%d", userID, time.Now().UnixNano())
}

func normalizeExpenseNotes(notes string) (string, bool) {
	normalizedNotes := strings.TrimSpace(notes)
	return normalizedNotes, len([]rune(normalizedNotes)) <= maxExpenseNotesLength
}

func createPaymentSplits(tx *gorm.DB, expenseID uint, inputs []PaymentSplitInput) error {
	splits := make([]PaymentSplit, 0, len(inputs))
	for _, input := range inputs {
		splits = append(splits, PaymentSplit{ExpenseID: expenseID, PaymentSource: input.PaymentSource, Amount: input.Amount})
	}
	if len(splits) == 0 {
		return nil
	}
	return tx.Create(&splits).Error
}

func replacePaymentSplits(tx *gorm.DB, expenseID uint, inputs []PaymentSplitInput) error {
	if err := tx.Where("expense_id = ?", expenseID).Delete(&PaymentSplit{}).Error; err != nil {
		return err
	}
	return createPaymentSplits(tx, expenseID, inputs)
}

func RegisterRoutes(rg *gin.RouterGroup) {
	expensesGroup := rg.Group("/expenses")
	{
		expensesGroup.POST("/", createExpense)
		expensesGroup.GET("/", getExpenses)
		expensesGroup.GET("/:id", getExpenseByID)
		expensesGroup.PATCH("/:id", updateExpense)
		expensesGroup.PATCH("/:id/payment-status", updatePaymentStatus)
		expensesGroup.PATCH("/:id/advance-status", updateAdvanceStatus)
		expensesGroup.DELETE("/:id", deleteExpense)
	}
}

func getUserID(c *gin.Context) (uint, bool) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	return userIDObj.(uint), true
}

func createExpense(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}

	var req CreateExpenseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos: " + err.Error()})
		return
	}
	req.Description = strings.TrimSpace(req.Description)
	if req.Description == "" || req.CategoryID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Descricao e categoria existente sao obrigatorias"})
		return
	}
	var category categories.Category
	if err := database.DB.Where("id = ? AND user_id = ?", req.CategoryID, userID).First(&category).Error; err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": "Nao foi possivel validar a categoria do usuario"})
		return
	}

	notes, ok := normalizeExpenseNotes(req.Notes)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Observações devem ter no máximo 500 caracteres"})
		return
	}

	expenseType := normalizeExpenseType(req.Type)
	if expenseType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tipo inválido. Use Única, Parcelada ou Fixa."})
		return
	}

	installments := req.Installments
	if installments <= 0 {
		installments = 1
	}

	loopCount := 1
	seriesID := ""

	switch expenseType {
	case "Única":
		installments = 1

	case "Parcelada":
		loopCount = installments
		seriesID = buildSeriesID(userID)

	case "Fixa":
		loopCount = fixedExpenseHorizonMonths
		installments = 0
		seriesID = buildSeriesID(userID)
	}

	splits, legacySource, err := ValidatePaymentSplits(req.Amount, req.PaymentSource, req.PaymentSplits)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		for i := 1; i <= loopCount; i++ {
			installmentDate := req.Date.AddDate(0, i-1, 0)
			newExpense := Expense{
				UserID:         userID,
				SeriesID:       seriesID,
				Amount:         req.Amount,
				Description:    req.Description,
				Notes:          notes,
				CategoryID:     req.CategoryID,
				PaymentSource:  legacySource,
				Date:           installmentDate,
				Month:          int(installmentDate.Month()),
				Year:           installmentDate.Year(),
				Type:           expenseType,
				Installments:   installments,
				CurrentInstall: i,
			}
			if err := tx.Create(&newExpense).Error; err != nil {
				return fmt.Errorf("erro ao salvar parcela %d: %w", i, err)
			}
			if err := createPaymentSplits(tx, newExpense.ID, splits); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar despesa(s)"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Despesa(s) cadastrada(s) com sucesso!"})
}

func getExpenses(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}

	var expensesList []Expense
	query := database.DB.Where("user_id = ?", userID)
	monthStr := c.Query("month")
	yearStr := c.Query("year")
	periodMode := strings.TrimSpace(strings.ToLower(c.DefaultQuery("period_mode", "scheduled")))
	if periodMode != "scheduled" && periodMode != "effective" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "period_mode invalido. Use scheduled ou effective"})
		return
	}

	if periodMode == "effective" {
		month, monthErr := strconv.Atoi(monthStr)
		year, yearErr := strconv.Atoi(yearStr)
		if monthErr != nil || month < 1 || month > 12 || yearErr != nil || year < 2000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "month e year validos sao obrigatorios com period_mode=effective"})
			return
		}
		query = ApplyEffectivePeriod(query, month, year)
	} else {
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
	}
	typeStr := c.Query("type")

	if typeStr != "" {
		normalizedType := normalizeExpenseType(typeStr)
		if normalizedType == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Tipo inválido. Use Única, Parcelada ou Fixa."})
			return
		}
		query = query.Where("type = ?", normalizedType)
	}

	if isPaid, filterByPaymentStatus, err := parsePaymentStatus(c.Query("payment_status")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	} else if filterByPaymentStatus {
		query = query.Where("is_paid = ?", isPaid)
	}

	if err := query.Preload("PaymentSplits").Find(&expensesList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar despesas"})
		return
	}
	HydrateExpensesPaymentSplits(expensesList)

	c.JSON(http.StatusOK, gin.H{"total": len(expensesList), "expenses": expensesList})
}

func parsePaymentStatus(value string) (isPaid bool, enabled bool, err error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "":
		return false, false, nil
	case "paid", "paga", "pago":
		return true, true, nil
	case "pending", "pendente":
		return false, true, nil
	default:
		return false, false, fmt.Errorf("Status de pagamento invalido. Use paid ou pending.")
	}
}

func getExpenseByID(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}

	id := c.Param("id")
	var expense Expense

	if err := database.DB.Preload("PaymentSplits").Where("id = ? AND user_id = ?", id, userID).First(&expense).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Despesa não encontrada ou não pertence a você"})
		return
	}
	HydratePaymentSplits(&expense)

	c.JSON(http.StatusOK, expense)
}

func updatePaymentStatus(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return
	}

	var req UpdatePaymentStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.IsPaid == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados invalidos: is_paid e obrigatorio"})
		return
	}

	var expense Expense
	if err := database.DB.Preload("PaymentSplits").Where("id = ? AND user_id = ?", c.Param("id"), userID).First(&expense).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Despesa nao encontrada ou nao pertence a voce"})
		return
	}

	updates := map[string]interface{}{"is_paid": *req.IsPaid}
	if *req.IsPaid {
		now := time.Now()
		updates["paid_at"] = now
		expense.PaidAt = &now
	} else {
		updates["paid_at"] = nil
		expense.PaidAt = nil
	}
	expense.IsPaid = *req.IsPaid

	if err := database.DB.Model(&expense).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao atualizar status de pagamento"})
		return
	}
	HydratePaymentSplits(&expense)

	c.JSON(http.StatusOK, gin.H{
		"message": "Status de pagamento atualizado com sucesso!",
		"expense": expense,
	})
}

func updateAdvanceStatus(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return
	}

	var req UpdateAdvanceStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.IsAdvanced == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados invalidos: is_advanced e obrigatorio"})
		return
	}

	var expense Expense
	if err := database.DB.Preload("PaymentSplits").Where("id = ? AND user_id = ?", c.Param("id"), userID).First(&expense).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Despesa nao encontrada ou nao pertence a voce"})
		return
	}

	updates := map[string]interface{}{"is_advanced": *req.IsAdvanced}
	if *req.IsAdvanced {
		if !CanBeAdvanced(expense.Type) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Somente despesas unicas ou parceladas podem ser adiantadas"})
			return
		}

		advancedAt, err := parseAdvanceDate(req.AdvancedAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := validateAdvanceDate(advancedAt, expense.Date); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		updates["advanced_at"] = advancedAt
		expense.AdvancedAt = &advancedAt
	} else {
		updates["advanced_at"] = nil
		expense.AdvancedAt = nil
	}
	expense.IsAdvanced = *req.IsAdvanced

	if err := database.DB.Model(&expense).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao atualizar adiantamento da despesa"})
		return
	}
	HydratePaymentSplits(&expense)

	c.JSON(http.StatusOK, gin.H{
		"message": "Adiantamento da despesa atualizado com sucesso!",
		"expense": expense,
	})
}

func parseAdvanceDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("advanced_at e obrigatorio ao adiantar uma despesa")
	}

	if parsed, err := time.ParseInLocation("2006-01-02", value, time.Local); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("advanced_at deve usar o formato YYYY-MM-DD ou RFC3339")
}

func validateAdvanceDate(advancedAt time.Time, scheduledAt time.Time) error {
	advancedDay := calendarDay(advancedAt)
	if !advancedDay.Before(calendarDay(scheduledAt)) {
		return fmt.Errorf("A data do adiantamento deve ser anterior a data prevista da despesa")
	}
	return nil
}

func calendarDay(value time.Time) time.Time {
	local := value.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

func updateExpense(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}

	id := c.Param("id")
	var expense Expense

	if err := database.DB.Preload("PaymentSplits").Where("id = ? AND user_id = ?", id, userID).First(&expense).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Despesa não encontrada ou não pertence a você"})
		return
	}

	originalMonth := expense.Month
	originalYear := expense.Year

	var updateData map[string]interface{}
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos: " + err.Error()})
		return
	}
	for key := range updateData {
		switch key {
		case "description", "category_id", "amount", "notes", "payment_source", "payment_splits", "date", "type", "installments", "update_future", "is_paid", "paid_at", "month", "year", "series_id", "current_install":
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Campo nao permitido na edicao: " + key})
			return
		}
		if strings.EqualFold(key, "is_advanced") || strings.EqualFold(key, "advanced_at") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Use a rota advance-status para alterar o adiantamento"})
			return
		}
	}
	if value, exists := updateData["description"]; exists {
		description, ok := value.(string)
		if !ok || strings.TrimSpace(description) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Descricao nao pode ser vazia"})
			return
		}
		updateData["description"] = strings.TrimSpace(description)
	}
	if value, exists := updateData["category_id"]; exists {
		categoryID, ok := value.(float64)
		if !ok || categoryID <= 0 || categoryID > 1e9 || categoryID != float64(uint(categoryID)) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Categoria invalida"})
			return
		}
		var category categories.Category
		if err := database.DB.Where("id = ? AND user_id = ?", uint(categoryID), userID).First(&category).Error; err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, gorm.ErrRecordNotFound) {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"error": "Nao foi possivel validar a categoria do usuario"})
			return
		}
	}

	var requestedSplits []PaymentSplitInput
	hasPaymentSplits := false
	if value, exists := updateData["payment_splits"]; exists {
		hasPaymentSplits = true
		encoded, err := json.Marshal(value)
		if err != nil || json.Unmarshal(encoded, &requestedSplits) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Divisões de pagamento inválidas"})
			return
		}
		delete(updateData, "payment_splits")
		if _, sendsLegacySource := updateData["payment_source"]; sendsLegacySource {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Envie payment_source ou payment_splits, não ambos"})
			return
		}
	}

	legacySource, hasLegacySource := updateData["payment_source"]
	if hasLegacySource {
		source, ok := legacySource.(string)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Origem de pagamento inválida"})
			return
		}
		legacySource = source
	}

	updateFuture := false
	if val, exists := updateData["update_future"]; exists {
		if boolVal, ok := val.(bool); ok {
			updateFuture = boolVal
		}
		delete(updateData, "update_future")
	}

	if notesValue, exists := updateData["notes"]; exists {
		if notesValue == nil {
			updateData["notes"] = ""
		} else {
			notesText, ok := notesValue.(string)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Observações inválidas"})
				return
			}

			notes, valid := normalizeExpenseNotes(notesText)
			if !valid {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Observações devem ter no máximo 500 caracteres"})
				return
			}
			updateData["notes"] = notes
		}
	}

	if typeValue, exists := updateData["type"]; exists {
		typeStr, ok := typeValue.(string)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Tipo inválido. Use Única, Parcelada ou Fixa."})
			return
		}

		normalizedType := normalizeExpenseType(typeStr)
		if normalizedType == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Tipo inválido. Use Única, Parcelada ou Fixa."})
			return
		}

		updateData["type"] = normalizedType
		if expense.IsAdvanced && normalizedType == "Fixa" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Remova o adiantamento antes de transformar a despesa em fixa"})
			return
		}

		if normalizedType == "Única" {
			updateData["installments"] = 1
		}
		if normalizedType == "Fixa" {
			updateData["installments"] = 0
		}
	}

	if dateStr, ok := updateData["date"].(string); ok {
		if parsedDate, err := time.Parse(time.RFC3339, dateStr); err == nil {
			if expense.IsAdvanced && expense.AdvancedAt != nil && !calendarDay(*expense.AdvancedAt).Before(calendarDay(parsedDate)) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "A nova data prevista deve ser posterior a data do adiantamento"})
				return
			}
			updateData["date"] = parsedDate
			updateData["month"] = int(parsedDate.Month())
			updateData["year"] = parsedDate.Year()
		}
	}

	if expense.Type == "Fixa" {
		updateData["installments"] = 0
	}

	var normalizedSplits []PaymentSplitInput
	shouldReplacePaymentSplits := false
	amount := expense.Amount
	if value, exists := updateData["amount"]; exists {
		parsed, ok := value.(float64)
		if !ok || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Valor da despesa inválido"})
			return
		}
		amount = parsed
	}
	if hasPaymentSplits {
		var err error
		normalizedSplits, _, err = ValidatePaymentSplits(amount, "", requestedSplits)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Keep the legacy field meaningful for old app versions.
		updateData["payment_source"] = normalizedSplits[0].PaymentSource
		shouldReplacePaymentSplits = true
	} else if hasLegacySource {
		var err error
		normalizedSplits, _, err = ValidatePaymentSplits(amount, legacySource.(string), nil)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		updateData["payment_source"] = normalizedSplits[0].PaymentSource
		shouldReplacePaymentSplits = true
	} else if _, amountIsChanging := updateData["amount"]; amountIsChanging && len(expense.PaymentSplits) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Envie payment_splits ao alterar o valor de uma despesa com divisão de pagamento"})
		return
	}

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&expense).Updates(updateData).Error; err != nil {
			return err
		}
		if shouldReplacePaymentSplits {
			if err := replacePaymentSplits(tx, expense.ID, normalizedSplits); err != nil {
				return err
			}
		}

		if !updateFuture {
			return nil
		}
		futureData := make(map[string]interface{}, len(updateData))
		for key, value := range updateData {
			futureData[key] = value
		}
		delete(futureData, "date")
		delete(futureData, "month")
		delete(futureData, "year")
		delete(futureData, "id")
		delete(futureData, "user_id")
		delete(futureData, "series_id")
		delete(futureData, "current_installment")

		var futureExpenses []Expense
		if err := applySeriesScope(tx, expense).
			Where("user_id = ? AND (year > ? OR (year = ? AND month > ?))", userID, originalYear, originalYear, originalMonth).
			Find(&futureExpenses).Error; err != nil {
			return err
		}
		if len(futureExpenses) == 0 {
			return nil
		}
		if err := applySeriesScope(tx.Model(&Expense{}), expense).
			Where("user_id = ? AND (year > ? OR (year = ? AND month > ?))", userID, originalYear, originalYear, originalMonth).
			Updates(futureData).Error; err != nil {
			return err
		}
		if shouldReplacePaymentSplits {
			for _, future := range futureExpenses {
				if err := replacePaymentSplits(tx, future.ID, normalizedSplits); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao atualizar despesa"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Despesa atualizada com sucesso!"})
}

func deleteExpense(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}

	id := c.Param("id")
	deleteFuture := c.Query("delete_future") == "true"

	var expense Expense
	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).First(&expense).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Despesa não encontrada ou não pertence a você"})
		return
	}

	if expense.Type == "Fixa" {
		deleteFuture = true
	}

	if deleteFuture {
		err := applySeriesScope(database.DB, expense).
			Where("user_id = ? AND (year > ? OR (year = ? AND month >= ?))",
				userID, expense.Year, expense.Year, expense.Month).
			Delete(&Expense{}).Error

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao deletar despesas em lote"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Despesa atual e todas as futuras foram removidas!"})
		return
	}

	if err := database.DB.Delete(&expense).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao deletar despesa"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Despesa deletada com sucesso!"})
}

func applySeriesScope(query *gorm.DB, expense Expense) *gorm.DB {
	if expense.SeriesID != "" {
		return query.Where("series_id = ?", expense.SeriesID)
	}
	return query.Where("description = ?", expense.Description)
}
