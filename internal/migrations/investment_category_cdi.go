package migrations

import "gorm.io/gorm"

const investmentCategoryCDISQL = `
INSERT INTO investment_categories (name) VALUES ('CDI') ON CONFLICT DO NOTHING;
`

func addInvestmentCategoryCDI(db *gorm.DB) error { return db.Exec(investmentCategoryCDISQL).Error }
