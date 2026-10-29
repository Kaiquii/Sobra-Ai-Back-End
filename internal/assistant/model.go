package assistant

import "time"

type Conversation struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"index"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	ConversationID uint      `json:"conversation_id" gorm:"index"`
	UserID         uint      `json:"user_id" gorm:"index"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	ToolCall       string    `json:"tool_call,omitempty"`
	ToolResult     string    `json:"tool_result,omitempty" gorm:"type:text"`
	CreatedAt      time.Time `json:"created_at"`
}

type ConversationResponse struct {
	ConversationID uint      `json:"conversation_id"`
	Title          string    `json:"title"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	DisplayDate    string    `json:"display_date"`
	DisplayTime    string    `json:"display_time"`
	DisplayLabel   string    `json:"display_label"`
}

type MessageResponse struct {
	MessageID      uint      `json:"message_id"`
	ConversationID uint      `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	ToolCall       string    `json:"tool_call,omitempty"`
	ToolResult     string    `json:"tool_result,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	DisplayTime    string    `json:"display_time"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Message        string        `json:"message" binding:"required"`
	ConversationID uint          `json:"conversation_id"`
	History        []ChatMessage `json:"history"`
	ModelID        *string       `json:"model_id,omitempty"`
}

type ChatResponse struct {
	ConversationID    uint        `json:"conversation_id"`
	Reply             string      `json:"reply"`
	ToolCall          string      `json:"tool_call,omitempty"`
	ToolResult        interface{} `json:"tool_result,omitempty"`
	ErrorCode         string      `json:"error_code,omitempty"`
	RetryAfterSeconds int         `json:"retry_after_seconds,omitempty"`
}

type GeminiRequest struct {
	SystemInstruction *GeminiContent  `json:"systemInstruction,omitempty"`
	Contents          []GeminiContent `json:"contents"`
	Tools             []GeminiTool    `json:"tools,omitempty"`
	GenerationConfig  map[string]any  `json:"generationConfig,omitempty"`
	CategoryNames     []string        `json:"-"`
	DebugStage        string          `json:"-"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text         string        `json:"text,omitempty"`
	FunctionCall *FunctionCall `json:"functionCall,omitempty"`
}

type FunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type GeminiTool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations"`
}

type FunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content GeminiContent `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
