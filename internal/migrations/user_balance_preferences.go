package migrations

import "gorm.io/gorm"

const userBalancePreferencesSQL = `
CREATE TABLE IF NOT EXISTS user_balance_preferences (
    user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    exclude_va_from_available boolean NOT NULL DEFAULT false,
    exclude_vr_from_available boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);
`

func addUserBalancePreferences(db *gorm.DB) error { return db.Exec(userBalancePreferencesSQL).Error }
