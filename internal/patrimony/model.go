package patrimony

import "time"

// Amounts are stored as integer cents; only the current balance is retained.
type FGTSBalance struct {
	UserID      uint `gorm:"primaryKey"`
	AmountCents int64
	UpdatedAt   time.Time
}

type Investment struct {
	ID          uint `gorm:"primaryKey"`
	UserID      uint
	Name        string
	Category    string
	CategoryID  uint
	AmountCents int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type investmentResponse struct {
	ID         uint      `json:"id"`
	Name       string    `json:"name"`
	Category   string    `json:"category"`
	CategoryID uint      `json:"category_id"`
	Amount     float64   `json:"amount"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func investmentData(item Investment) investmentResponse {
	return investmentResponse{ID: item.ID, Name: item.Name, Category: item.Category, CategoryID: item.CategoryID, Amount: float64(item.AmountCents) / 100, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}
