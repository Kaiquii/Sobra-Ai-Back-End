package migrations

import "gorm.io/gorm"

// Frozen additive migration: no changes to monthly income/expense tables.
const patrimonySQL = `
CREATE TABLE fgts_balances (
 user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 amount_cents BIGINT NOT NULL CHECK (amount_cents BETWEEN 0 AND 999999999999),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE investments (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name VARCHAR(120) NOT NULL CHECK (length(trim(name)) > 0),
 category VARCHAR(80) NOT NULL CHECK (length(trim(category)) > 0),
 amount_cents BIGINT NOT NULL CHECK (amount_cents BETWEEN 0 AND 999999999999),
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_investments_user_id ON investments(user_id);
`

func addPatrimony(db *gorm.DB) error { return db.Exec(patrimonySQL).Error }
