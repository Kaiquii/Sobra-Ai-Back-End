package assistant

import (
	"Sobra_Ai_Back-end/internal/database"
	"encoding/json"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func getOrCreateConversation(userID uint, conversationID uint, firstMessage string) (Conversation, error) {
	if conversationID != 0 {
		var conversation Conversation
		err := database.DB.Where("id = ? AND user_id = ?", conversationID, userID).First(&conversation).Error
		return conversation, err
	}

	title := strings.TrimSpace(firstMessage)
	if len(title) > 60 {
		title = title[:60]
	}
	if title == "" {
		title = "Nova conversa"
	}

	conversation := Conversation{
		UserID: userID,
		Title:  title,
	}
	err := database.DB.Create(&conversation).Error
	return conversation, err
}

func saveMessage(userID uint, conversationID uint, role string, content string, toolCall string, toolResult any) error {
	return saveMessageDB(database.DB, userID, conversationID, role, content, toolCall, toolResult)
}

func saveMessageDB(db *gorm.DB, userID uint, conversationID uint, role string, content string, toolCall string, toolResult any) error {
	var toolResultJSON string
	if toolResult != nil {
		resultBytes, err := json.Marshal(toolResult)
		if err != nil {
			return err
		}
		toolResultJSON = string(resultBytes)
	}

	message := Message{
		ConversationID: conversationID,
		UserID:         userID,
		Role:           role,
		Content:        strings.TrimSpace(content),
		ToolCall:       toolCall,
		ToolResult:     toolResultJSON,
	}

	if err := db.Create(&message).Error; err != nil {
		return err
	}

	return db.Model(&Conversation{}).
		Where("id = ? AND user_id = ?", conversationID, userID).
		Update("updated_at", message.CreatedAt).Error
}

func loadRecentMessages(userID uint, conversationID uint, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 20
	}

	var newest []Message
	err := database.DB.
		Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Order("id desc").
		Limit(limit).
		Find(&newest).Error
	if err != nil {
		return nil, err
	}

	messages := make([]Message, len(newest))
	for i := range newest {
		messages[len(newest)-1-i] = newest[i]
	}
	return messages, nil
}

func buildConversationFromMessages(messages []Message, currentMessage string) []GeminiContent {
	// Bound cost and keep the newest server-owned context. Never trust client
	// history or promote stored text into system/developer instructions.
	if len(messages) > 20 {
		messages = messages[len(messages)-20:]
	}
	budget := 24000
	start := len(messages)
	for start > 0 {
		length := len(messages[start-1].Content)
		if length > budget {
			break
		}
		budget -= length
		start--
	}
	messages = messages[start:]
	contents := make([]GeminiContent, 0, len(messages)+1)
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" && message.Role != "model" {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}

		role := "user"
		if message.Role == "assistant" || message.Role == "model" {
			role = "model"
		}

		contents = append(contents, GeminiContent{
			Role:  role,
			Parts: []GeminiPart{{Text: content}},
		})
	}

	contents = append(contents, GeminiContent{
		Role:  "user",
		Parts: []GeminiPart{{Text: strings.TrimSpace(currentMessage)}},
	})

	return contents
}

// The fresh snapshot replaces old report figures, but keeps period/category
// references needed by follow-ups. User text and conversational analysis stay.
func compactFinancialHistory(messages []Message) []Message {
	result := append([]Message(nil), messages...)
	for i := range result {
		message := &result[i]
		if message.Role != "assistant" || (!strings.HasPrefix(message.ToolCall, "get_") && message.ToolCall != "financial_analysis") {
			continue
		}
		var data map[string]any
		if json.Unmarshal([]byte(message.ToolResult), &data) != nil {
			continue
		}
		if message.ToolCall == "financial_analysis" {
			if data["status"] != "read_only" {
				continue
			}
			metadata := map[string]any{"executed_query": data["query"]}
			if data["query"] == nil {
				if data["reply_source"] != "verified_summary" {
					continue // Keep conversational advice without an executed plan.
				}
				metadata = map[string]any{"verified_summary": true, "years": data["years"], "as_of": data["as_of"]}
			}
			encoded, err := json.Marshal(metadata)
			if err == nil {
				message.Content = "Consulta anterior executada (dados; nao substitui o pedido do usuario): " + string(encoded)
			}
			continue
		}
		context := map[string]any{"query": message.ToolCall}
		for _, key := range []string{"month", "year", "category_name", "payment_source"} {
			if value, ok := data[key]; ok {
				context[key] = value
			}
		}
		encoded, err := json.Marshal(context)
		if err == nil {
			message.Content = "Consulta anterior (use os fatos atuais para valores): " + string(encoded)
		}
	}
	return result
}

func lastPendingCreateExpense(userID uint, conversationID uint) (map[string]any, bool) {
	result, ok, _ := pendingCreateExpenseDB(database.DB, userID, conversationID)
	return result, ok
}

func pendingCreateExpenseDB(db *gorm.DB, userID uint, conversationID uint) (map[string]any, bool, error) {
	var messages []Message
	err := db.
		Where("conversation_id = ? AND user_id = ? AND role = ? AND tool_call = ?", conversationID, userID, "assistant", "create_expense").
		Order("id desc").
		Limit(1).
		Find(&messages).Error
	if err != nil {
		return nil, false, err
	}
	if len(messages) == 0 || messages[0].ToolResult == "" {
		return nil, false, nil
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(messages[0].ToolResult), &result); err != nil {
		return nil, false, err
	}

	status, _ := result["status"].(string)
	if status != "needs_confirmation" && status != "needs_information" {
		return nil, false, nil
	}
	if version, _ := intArg(result, "draft_version"); version != expenseDraftVersion {
		// Legacy amount meant per-installment value; ask for the total again.
		result["amount"] = 0
		result["status"] = "needs_information"
	}
	return result, true, nil
}

// Serialize confirmations and consume the proposal in the same transaction as
// the expenses and reply. Concurrent/retried confirmations cannot create twice.
func confirmPendingExpense(userID, conversationID uint, message string) (map[string]any, bool, error) {
	var result map[string]any
	handled := false
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		var conversation Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", conversationID, userID).First(&conversation).Error; err != nil {
			return err
		}
		pending, ok, err := pendingCreateExpenseDB(tx, userID, conversationID)
		if err != nil || !ok {
			return err
		}
		handled = true
		pending["confirm"] = pending["status"] == "needs_confirmation"
		result, err = createExpenseFromToolDB(tx, userID, pending)
		if err != nil {
			return err
		}
		if err := saveMessageDB(tx, userID, conversationID, "user", message, "", nil); err != nil {
			return err
		}
		return saveMessageDB(tx, userID, conversationID, "assistant", localToolReply("create_expense", result), "create_expense", result)
	})
	return result, handled, err
}
