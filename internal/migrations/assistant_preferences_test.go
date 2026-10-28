package migrations

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestAssistantModelPreferencesMigrationChecksum(t *testing.T) {
	for _, migration := range sortedRegistry() {
		if migration.Version == 9 {
			if migration.Name != "add_assistant_model_preferences" || migration.Checksum != fmt.Sprintf("%x", sha256.Sum256([]byte(assistantPreferencesSQL))) {
				t.Fatal("assistant model preferences migration SQL or checksum changed")
			}
			return
		}
	}
	t.Fatal("missing assistant model preferences migration")
}
