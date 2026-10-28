package assistant

import (
	"Sobra_Ai_Back-end/internal/database"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(rg *gin.RouterGroup) {
	assistantGroup := rg.Group("/assistant")
	{
		assistantGroup.POST("/chat", chat)
		assistantGroup.GET("/conversations", listConversations)
		assistantGroup.GET("/conversations/:id/messages", listMessages)
		assistantGroup.DELETE("/conversations/:id", deleteConversation)
		assistantGroup.GET("/models", listAssistantModels)
		assistantGroup.GET("/model-preference", getAssistantModelPreference)
		assistantGroup.PUT("/model-preference", updateAssistantModelPreference)
	}
}

func chat(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 55*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	userIDObj, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return
	}
	userID := userIDObj.(uint)
	c.Request = c.Request.WithContext(withAssistantUsage(c.Request.Context(), userID))

	var req ChatRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mensagem invalida"})
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" || len(req.Message) > 8000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Informe uma mensagem de ate 8000 bytes"})
		return
	}

	conversation, err := getOrCreateConversation(userID, req.ConversationID, req.Message)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversa nao encontrada"})
		return
	}

	if isConfirmationMessage(req.Message) {
		toolResult, handled, err := confirmPendingExpense(userID, conversation.ID, req.Message)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao confirmar despesa; nenhuma alteracao foi gravada"})
			return
		}
		if handled {
			c.JSON(http.StatusOK, ChatResponse{ConversationID: conversation.ID, Reply: localToolReply("create_expense", toolResult), ToolCall: "create_expense", ToolResult: toolResult})
			return
		}
		c.JSON(http.StatusOK, ChatResponse{ConversationID: conversation.ID, Reply: "Nao ha cadastro pendente para confirmar. Informe os dados da despesa."})
		return
	}
	// Omitted/blank model_id always delegates routing to the backend. Old saved
	// preferences remain available to legacy endpoints but never pin the new app.
	selected := ""
	if req.ModelID != nil {
		selected = strings.TrimSpace(*req.ModelID)
		if selected != "" {
			models, catalogErr := listFreeAssistantModels(c.Request.Context())
			if catalogErr != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Catalogo de modelos temporariamente indisponivel", "error_code": "assistant_unavailable"})
				return
			}
			allowed := false
			for _, model := range models {
				allowed = allowed || model.ID == selected
			}
			if !allowed {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Selecione um modelo gratuito disponivel", "error_code": "invalid_model"})
				return
			}
		}
	}
	client, err := newAssistantGeneratorForModel(c.Request.Context(), selected)
	if err != nil {
		reply, code, retry := assistantErrorResponse(err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": reply, "error_code": code, "retry_after_seconds": retry})
		return
	}

	recentMessages, err := loadRecentMessages(userID, conversation.ID, 20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao carregar historico"})
		return
	}

	pending, _, err := pendingCreateExpenseDB(database.DB, userID, conversation.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao carregar rascunho"})
		return
	}
	handled, reply, toolCall, toolResult := handleFinancialMessageWithHistory(c.Request.Context(), client, userID, req.Message, recentMessages, pending)
	if handled {
		errorCode, retryAfter := responseErrorFields(toolResult)
		if errorCode != "" {
			log.Printf("Assistente retornou erro controlado: %s retry_after=%d", errorCode, retryAfter)
		}
		if err := saveMessage(userID, conversation.ID, "user", req.Message, "", nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar mensagem"})
			return
		}
		if err := saveMessage(userID, conversation.ID, "assistant", reply, toolCall, toolResult); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar resposta"})
			return
		}

		c.JSON(http.StatusOK, ChatResponse{
			ConversationID:    conversation.ID,
			Reply:             reply,
			ToolCall:          toolCall,
			ToolResult:        toolResult,
			ErrorCode:         errorCode,
			RetryAfterSeconds: retryAfter,
		})
		return
	}

	contents := buildConversationFromMessages(recentMessages, req.Message)
	request := GeminiRequest{
		SystemInstruction: &GeminiContent{Parts: []GeminiPart{{Text: systemPrompt()}}},
		Contents:          contents,
		GenerationConfig:  map[string]any{"temperature": 0.2},
		DebugStage:        "assistant_chat",
	}

	reply, err = client.generateText(request)
	if err != nil {
		log.Printf("Assistente: falha na resposta conversacional")
		reply, errorCode, retryAfter := assistantErrorResponse(err)
		_ = saveMessage(userID, conversation.ID, "user", req.Message, "", nil)
		_ = saveMessage(userID, conversation.ID, "assistant", reply, "", nil)
		c.JSON(http.StatusOK, ChatResponse{
			ConversationID:    conversation.ID,
			Reply:             reply,
			ErrorCode:         errorCode,
			RetryAfterSeconds: retryAfter,
		})
		return
	}

	if err := saveMessage(userID, conversation.ID, "user", req.Message, "", nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar mensagem"})
		return
	}

	if reply == "" {
		reply = "Nao consegui interpretar sua mensagem agora. Pode tentar perguntar de outro jeito?"
	}

	if err := saveMessage(userID, conversation.ID, "assistant", reply, "", nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar resposta"})
		return
	}

	c.JSON(http.StatusOK, ChatResponse{ConversationID: conversation.ID, Reply: reply})
}

func listConversations(c *gin.Context) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return
	}
	userID := userIDObj.(uint)

	var conversations []Conversation
	if err := database.DB.
		Where("user_id = ?", userID).
		Order("updated_at desc").
		Find(&conversations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar conversas"})
		return
	}

	response := make([]ConversationResponse, 0, len(conversations))
	for _, conversation := range conversations {
		response = append(response, conversationResponse(conversation))
	}

	c.JSON(http.StatusOK, gin.H{"total": len(response), "conversations": response})
}

func listMessages(c *gin.Context) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return
	}
	userID := userIDObj.(uint)

	conversationID := c.Param("id")
	var conversation Conversation
	if err := database.DB.Where("id = ? AND user_id = ?", conversationID, userID).First(&conversation).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversa nao encontrada"})
		return
	}

	var messages []Message
	if err := database.DB.
		Where("conversation_id = ? AND user_id = ?", conversation.ID, userID).
		Order("created_at asc").
		Find(&messages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar mensagens"})
		return
	}

	response := make([]MessageResponse, 0, len(messages))
	for _, message := range messages {
		response = append(response, messageResponse(message))
	}

	c.JSON(http.StatusOK, gin.H{"conversation": conversationResponse(conversation), "messages": response})
}

func deleteConversation(c *gin.Context) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return
	}
	userID := userIDObj.(uint)

	conversationID := c.Param("id")
	tx := database.DB.Begin()
	if err := tx.Where("conversation_id = ? AND user_id = ?", conversationID, userID).Delete(&Message{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao apagar mensagens"})
		return
	}
	if err := tx.Where("id = ? AND user_id = ?", conversationID, userID).Delete(&Conversation{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao apagar conversa"})
		return
	}
	tx.Commit()

	c.JSON(http.StatusOK, gin.H{"message": "Conversa apagada com sucesso"})
}

func conversationResponse(conversation Conversation) ConversationResponse {
	return ConversationResponse{
		ConversationID: conversation.ID,
		Title:          conversation.Title,
		CreatedAt:      conversation.CreatedAt,
		UpdatedAt:      conversation.UpdatedAt,
		DisplayDate:    conversation.CreatedAt.Format("02/01/2006"),
		DisplayTime:    conversation.CreatedAt.Format("15:04"),
		DisplayLabel:   conversation.CreatedAt.Format("02/01/2006 15:04"),
	}
}

func messageResponse(message Message) MessageResponse {
	return MessageResponse{
		MessageID:      message.ID,
		ConversationID: message.ConversationID,
		Role:           message.Role,
		Content:        message.Content,
		ToolCall:       message.ToolCall,
		ToolResult:     message.ToolResult,
		CreatedAt:      message.CreatedAt,
		DisplayTime:    message.CreatedAt.Format("15:04"),
	}
}

func buildConversation(req ChatRequest) []GeminiContent {
	contents := make([]GeminiContent, 0, len(req.History)+1)

	for _, msg := range req.History {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}

		role := "user"
		if strings.EqualFold(msg.Role, "assistant") || strings.EqualFold(msg.Role, "model") {
			role = "model"
		}

		contents = append(contents, GeminiContent{
			Role:  role,
			Parts: []GeminiPart{{Text: content}},
		})
	}

	contents = append(contents, GeminiContent{
		Role:  "user",
		Parts: []GeminiPart{{Text: strings.TrimSpace(req.Message)}},
	})

	return contents
}

func summarizeToolResult(client textGenerator, userMessage string, toolName string, toolResult any) (string, error) {
	resultJSON, err := json.Marshal(toolResult)
	if err != nil {
		return "", err
	}

	prompt := "Mensagem do usuario: " + userMessage + "\n" +
		"Ferramenta executada: " + toolName + "\n" +
		"Resultado JSON: " + string(resultJSON) + "\n\n" +
		"Responda em portugues do Brasil, de forma curta e util. " +
		"Se o status for needs_confirmation, peca confirmacao antes de cadastrar. " +
		"Se o status for created, confirme o cadastro. " +
		"Nao invente valores fora do JSON."

	reply, err := client.generateText(GeminiRequest{
		SystemInstruction: &GeminiContent{Parts: []GeminiPart{{Text: "Voce transforma resultados JSON do SobraAí em respostas naturais, precisas e curtas.\n" + assistantReplyStyle}}},
		Contents: []GeminiContent{{
			Role:  "user",
			Parts: []GeminiPart{{Text: prompt}},
		}},
		GenerationConfig: map[string]any{"temperature": 0.2},
		DebugStage:       "assistant_summarize_tool",
	})
	if err != nil {
		return "", err
	}

	if reply == "" {
		return "Pronto, consultei os dados para voce.", nil
	}
	return reply, nil
}

func isConfirmationMessage(message string) bool {
	normalized := strings.TrimRight(strings.TrimSpace(strings.ToLower(message)), ".!?")
	confirmations := []string{
		"sim",
		"pode",
		"pode cadastrar",
		"confirmo",
		"confirmar",
		"isso",
		"isso mesmo",
		"correto",
		"esta correto",
		"ta certo",
		"ok",
		"beleza",
	}

	for _, confirmation := range confirmations {
		if normalized == confirmation {
			return true
		}
	}
	return false
}

func normalizeText(text string) string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	replacements := map[string]string{
		"á": "a", "à": "a", "ã": "a", "â": "a",
		"é": "e", "ê": "e",
		"í": "i",
		"ó": "o", "õ": "o", "ô": "o",
		"ú": "u",
		"ç": "c",
	}
	for from, to := range replacements {
		normalized = strings.ReplaceAll(normalized, from, to)
	}
	return normalized
}

func assistantErrorResponse(err error) (string, string, int) {
	if code, retryAfter, ok := openRouterErrorInfo(err); ok {
		if code == "assistant_user_limit" {
			return "Voce atingiu seu limite temporario de consultas com IA. Seus dados continuam disponiveis nas telas do app. Tente novamente mais tarde.", code, retryAfter
		}
		if code == "assistant_quota_exceeded" {
			return quotaReply(retryAfter), code, retryAfter
		}
		return "A IA esta temporariamente indisponivel. Tente novamente em alguns instantes.", code, retryAfter
	}

	return "Nao consegui falar com a IA agora. Tente novamente em alguns instantes.", "assistant_unavailable", 0
}

func responseErrorFields(toolResult any) (string, int) {
	result, ok := toolResult.(map[string]any)
	if !ok {
		return "", 0
	}

	errorCode, _ := result["error_code"].(string)
	retryAfter := int(numberToFloat(result["retry_after_seconds"]))
	return errorCode, retryAfter
}

func quotaReply(retryAfter int) string {
	if retryAfter > 0 {
		return "A IA atingiu o limite temporario de uso. Tente novamente em " + strconv.Itoa(retryAfter) + " segundos."
	}
	return "A IA atingiu o limite temporario de uso. Tente novamente mais tarde. Seus dados continuam disponiveis no aplicativo."
}

func localToolReply(toolName string, toolResult any) string {
	result, ok := toolResult.(map[string]any)
	if !ok {
		return "Pronto, consultei os dados para voce."
	}

	if toolName == "create_expense" {
		status, _ := result["status"].(string)
		if status == "cancelled" {
			return "Cancelei o rascunho. Nenhuma despesa foi cadastrada."
		}
		if status == "needs_information" {
			labels := map[string]string{
				"description": "descricao da despesa", "category_name": "categoria existente escolhida por voce",
				"payment_source": "fonte de pagamento (Salario, Adiantamento, Renda Extra, VA ou VR)",
				"amount":         "valor positivo com ate duas casas decimais (total da compra, se parcelada)",
				"date":           "data completa valida (dia, mes e ano)",
				"type":           "tipo (Unica, Parcelada ou Fixa)", "installments": "quantidade de parcelas (2 a 120)",
			}
			var fields []string
			if missing, ok := result["missing_fields"].([]string); ok {
				for _, key := range missing {
					fields = append(fields, labels[key])
				}
			}
			return "Antes de cadastrar, preciso que informe ou corrija: " + strings.Join(fields, "; ") + ". Nenhuma despesa foi cadastrada."
		}
		description, _ := result["description"].(string)
		paymentSource, _ := result["payment_source"].(string)
		date, _ := result["date"].(string)
		amount := numberToFloat(result["amount"])
		category, _ := result["category_name"].(string)
		kind, _ := result["type"].(string)
		details := description + ", categoria " + category + ", tipo " + kind + ", fonte " + paymentSource + ", primeira data " + date
		if kind == "Parcelada" {
			count, _ := intArg(result, "installments")
			parts := []string{}
			if amounts, ok := result["installment_amounts"].([]float64); ok {
				for _, value := range amounts {
					parts = append(parts, "R$ "+formatMoney(value))
				}
			}
			details += ", total R$ " + formatMoney(amount) + " em " + strconv.Itoa(count) + " parcelas: " + strings.Join(parts, ", ")
		} else {
			details += ", valor R$ " + formatMoney(amount)
			if kind == "Fixa" {
				details += " por mes (120 lancamentos)"
			}
		}

		if status == "created" {
			return "Cadastrei: " + details + "."
		}

		return "Confira o cadastro: " + details + ". Posso confirmar?"
	}

	if toolName == "get_monthly_summary" {
		month := numberToFloat(result["month"])
		year := numberToFloat(result["year"])
		totalIncome := numberToFloat(result["total_income"])
		totalExpense := numberToFloat(result["total_expense"])
		totalSpentSalary := numberToFloat(result["total_gasto_salario"])
		totalSpentAdiantamento := numberToFloat(result["total_gasto_adiantamento"])
		totalSpentRendaExtra := numberToFloat(result["total_gasto_renda_extra"])
		totalAvailable := numberToFloat(result["total_geral_disponivel"])

		return "Em " + monthLabel(month, year) +
			", voce gastou R$ " + formatMoney(totalExpense) + ".\n\n" +
			"De onde saiu:\n" +
			"Salario: R$ " + formatMoney(totalSpentSalary) + "\n" +
			"Adiantamento: R$ " + formatMoney(totalSpentAdiantamento) + "\n" +
			"Renda Extra: R$ " + formatMoney(totalSpentRendaExtra) + "\n" +
			"VA: R$ " + formatMoney(numberToFloat(result["total_gasto_va"])) + "\n" +
			"VR: R$ " + formatMoney(numberToFloat(result["total_gasto_vr"])) + "\n\n" +
			"Entradas do mes: R$ " + formatMoney(totalIncome) + "\n" +
			"Saldo restante: R$ " + formatMoney(totalAvailable) + "."
	}

	if toolName == "get_category_expenses" {
		month := numberToFloat(result["month"])
		year := numberToFloat(result["year"])
		totalExpense := numberToFloat(result["total_expense"])
		percentage := numberToFloat(result["percentage"])
		categoryName, _ := result["category_name"].(string)
		paymentSource, _ := result["payment_source"].(string)

		sourceText := ""
		if strings.TrimSpace(paymentSource) != "" {
			sourceText = " paga com " + paymentSource
		}

		reply := "Em " + monthLabel(month, year) +
			", voce gastou R$ " + formatMoney(totalExpense) +
			" com " + categoryName + sourceText + "."

		if percentage > 0 {
			reply += "\nIsso representa " + formatPercentage(percentage) + "% dos seus gastos do mes."
		}

		return reply
	}

	if toolName == "list_categories" {
		names, ok := result["names"].([]string)
		if !ok || len(names) == 0 {
			return "Voce ainda nao tem categorias cadastradas."
		}
		return "Voce tem estas categorias cadastradas: " + strings.Join(names, ", ") + "."
	}

	if toolName == "get_all_category_expenses" {
		month := numberToFloat(result["month"])
		year := numberToFloat(result["year"])
		totalExpense := numberToFloat(result["total_expense"])
		items, _ := result["categories"].([]map[string]any)

		if len(items) == 0 {
			return "Nao encontrei gastos por categoria em " + strconv.Itoa(int(month)) + "/" + strconv.Itoa(int(year)) + "."
		}

		parts := make([]string, 0, len(items))
		for _, item := range items {
			categoryName, _ := item["category_name"].(string)
			totalAmount := numberToFloat(item["total_amount"])
			parts = append(parts, categoryName+": R$ "+formatMoney(totalAmount))
		}

		return "Em " + strconv.Itoa(int(month)) + "/" + strconv.Itoa(int(year)) +
			", voce gastou R$ " + formatMoney(totalExpense) +
			" no total. Por categoria: " + strings.Join(parts, "; ") + "."
	}

	return "Pronto, consultei os dados para voce."
}

func numberToFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	default:
		return 0
	}
}

func formatMoney(value float64) string {
	text := strconv.FormatFloat(value, 'f', 2, 64)
	parts := strings.Split(text, ".")
	integerPart := parts[0]
	decimalPart := "00"
	if len(parts) > 1 {
		decimalPart = parts[1]
	}

	sign := ""
	if strings.HasPrefix(integerPart, "-") {
		sign = "-"
		integerPart = strings.TrimPrefix(integerPart, "-")
	}

	var grouped []string
	for len(integerPart) > 3 {
		grouped = append([]string{integerPart[len(integerPart)-3:]}, grouped...)
		integerPart = integerPart[:len(integerPart)-3]
	}
	grouped = append([]string{integerPart}, grouped...)

	return sign + strings.Join(grouped, ".") + "," + decimalPart
}

func formatPercentage(value float64) string {
	text := strconv.FormatFloat(value, 'f', 2, 64)
	return strings.Replace(text, ".", ",", 1)
}

func monthLabel(month float64, year float64) string {
	months := []string{
		"",
		"janeiro",
		"fevereiro",
		"marco",
		"abril",
		"maio",
		"junho",
		"julho",
		"agosto",
		"setembro",
		"outubro",
		"novembro",
		"dezembro",
	}

	monthNumber := int(month)
	yearNumber := int(year)
	if monthNumber < 1 || monthNumber > 12 {
		return strconv.Itoa(monthNumber) + "/" + strconv.Itoa(yearNumber)
	}

	return months[monthNumber] + "/" + strconv.Itoa(yearNumber)
}

func systemPrompt() string {
	currentDate := time.Now().Format("2006-01-02")
	return "Voce e o assistente financeiro do SobraAí. Data atual: " + currentDate + ". " +
		"Responda apenas sobre o aplicativo e sobre os dados financeiros do usuario. " +
		"Para perguntas de como usar o app, explique de forma simples: o usuario pode cadastrar despesas, rendas, categorias, ver resumo mensal, gastos por categoria e graficos. " +
		"Para perguntas financeiras, use as ferramentas disponiveis em vez de inventar numeros. " +
		"No cadastro de despesas, nunca invente campos ausentes nem afirme que houve cadastro sem resultado created. A categoria deve existir e ter sido escolhida pelo usuario. " +
		"Nunca diga que acessou o banco diretamente; quem executa as consultas e o backend Go. " +
		"Para criar, atualizar ou deletar dados, nunca execute sem confirmacao clara do usuario. " +
		"Na ferramenta create_expense, use confirm=false para a primeira interpretacao do pedido. Use confirm=true somente se a mensagem atual confirmar claramente uma proposta anterior. " +
		"Se faltarem mes, ano, valor, descricao ou fonte de pagamento, pergunte antes de chamar ferramenta de cadastro.\n" + assistantReplyStyle
}
