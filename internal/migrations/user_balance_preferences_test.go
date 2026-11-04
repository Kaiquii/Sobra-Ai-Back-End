package migrations

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestUserBalancePreferencesMigrationChecksum(t *testing.T) {
	for _, migration := range sortedRegistry() {
		if migration.Version != 10 {
			continue
		}
		if migration.Name != "add_user_balance_preferences" || migration.Checksum != fmt.Sprintf("%x", sha256.Sum256([]byte(userBalancePreferencesSQL))) {
			t.Fatal("user balance preferences migration SQL or checksum changed")
		}
		return
	}
	t.Fatal("missing user balance preferences migration")
}

func TestSchemaNineRequiresOnlyUserBalancePreferencesMigration(t *testing.T) {
	applied := make(map[int64]AppliedMigration)
	for _, migration := range sortedRegistry() {
		if migration.Version <= 9 {
			applied[migration.Version] = AppliedMigration{
				Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum,
			}
		}
	}
	if err := validateAppliedMigrations(applied); err != nil {
		t.Fatal(err)
	}
	pending := pendingMigrations(applied)
	if len(pending) != 1 || pending[0].Version != 10 || pending[0].Name != "add_user_balance_preferences" {
		t.Fatalf("schema nine upgrade must apply only the new preference table: %+v", pending)
	}
}
