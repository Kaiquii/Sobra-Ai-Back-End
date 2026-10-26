package migrations

import "gorm.io/gorm"

// Category ownership is NULL for the shared, immutable built-in catalog.
const investmentCategoriesSQL = `
CREATE TABLE investment_categories (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
 name VARCHAR(80) NOT NULL CHECK (length(trim(name)) > 0)
);
CREATE UNIQUE INDEX idx_investment_categories_name
 ON investment_categories (COALESCE(user_id, 0), lower(trim(name)));
CREATE INDEX idx_investment_categories_user_id ON investment_categories(user_id);
INSERT INTO investment_categories (name) VALUES
 ('CDB'), ('RDB'), ('Tesouro Direto'), ('LCI'), ('LCA'), ('CRI'), ('CRA'),
 ('Poupança'), ('Fundos de investimento'), ('Ações'), ('Fundos imobiliários'),
 ('ETFs'), ('Previdência privada'), ('Outros');

-- Preserve clients and records using the previous category text contract.
INSERT INTO investment_categories(user_id, name)
 SELECT i.user_id, min(trim(i.category))
 FROM investments i
 WHERE NOT EXISTS (
  SELECT 1 FROM investment_categories c
  WHERE (c.user_id IS NULL OR c.user_id = i.user_id)
   AND lower(trim(c.name)) = lower(trim(i.category))
 )
 GROUP BY i.user_id, lower(trim(i.category))
 ON CONFLICT DO NOTHING;
ALTER TABLE investments ADD COLUMN category_id BIGINT;
UPDATE investments i SET category_id = (
 SELECT c.id FROM investment_categories c
 WHERE (c.user_id IS NULL OR c.user_id = i.user_id)
  AND lower(trim(c.name)) = lower(trim(i.category))
 ORDER BY c.user_id NULLS FIRST LIMIT 1
);
ALTER TABLE investments ALTER COLUMN category_id SET NOT NULL;
ALTER TABLE investments ADD CONSTRAINT fk_investments_category
 FOREIGN KEY (category_id) REFERENCES investment_categories(id)
 DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX idx_investments_user_category ON investments(user_id, category_id);
`

func addInvestmentCategories(db *gorm.DB) error { return db.Exec(investmentCategoriesSQL).Error }
