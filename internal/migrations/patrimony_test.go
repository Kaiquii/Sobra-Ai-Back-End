package migrations

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestPatrimonyMigrationChecksum(t *testing.T) {
	for _, migration := range sortedRegistry() {
		if migration.Version == 6 {
			if migration.Name != "add_patrimony" || migration.Checksum != fmt.Sprintf("%x", sha256.Sum256([]byte(patrimonySQL))) {
				t.Fatal("patrimony SQL or registry checksum changed")
			}
			return
		}
	}
	t.Fatal("missing patrimony migration")
}

func TestInvestmentCategoriesMigrationChecksum(t *testing.T) {
	for _, migration := range sortedRegistry() {
		if migration.Version == 7 {
			if migration.Name != "add_investment_categories" || migration.Checksum != fmt.Sprintf("%x", sha256.Sum256([]byte(investmentCategoriesSQL))) {
				t.Fatal("category SQL or checksum changed")
			}
			return
		}
	}
	t.Fatal("missing investment categories migration")
}

func TestInvestmentCategoryCDIMigrationChecksum(t *testing.T) {
	for _, migration := range sortedRegistry() {
		if migration.Version == 8 {
			if migration.Name != "add_investment_category_cdi" || migration.Checksum != fmt.Sprintf("%x", sha256.Sum256([]byte(investmentCategoryCDISQL))) {
				t.Fatal("CDI migration SQL or checksum changed")
			}
			return
		}
	}
	t.Fatal("missing CDI default migration")
}
