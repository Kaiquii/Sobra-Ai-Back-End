package migrations

import "gorm.io/gorm"

const assistantPreferencesSQL = `
CREATE TABLE IF NOT EXISTS assistant_model_preferences (
    user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    model_id varchar(160) NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);
`

func addAssistantModelPreferences(db *gorm.DB) error { return db.Exec(assistantPreferencesSQL).Error }
