package storage

import (
	"os"
	"testing"

	"gorm.io/gorm"
)

func TestGroupPriorityMigrationPreservesExistingData(t *testing.T) {
	testGroupPriorityMigration(t, openInternalMigrationTestDatabase)
}

func TestExternalGroupPriorityMigrationPreservesExistingData(t *testing.T) {
	dsn := os.Getenv("GPT_LOAD_DATABASE_TEST_DSN")
	if dsn == "" {
		t.Skip("GPT_LOAD_DATABASE_TEST_DSN is not set")
	}
	testGroupPriorityMigration(t, func(t *testing.T) *gorm.DB {
		return openExternalIncrementalMigrationDatabase(t, dsn)
	})
}

func testGroupPriorityMigration(t *testing.T, open func(*testing.T) *gorm.DB) {
	for _, interrupted := range []bool{false, true} {
		name := "upgrade"
		if interrupted {
			name = "resume"
		}
		t.Run(name, func(t *testing.T) {
			db := open(t)
			if err := applyMigrationRegistry(db, migrations[:28]); err != nil {
				t.Fatal(err)
			}
			row := map[string]any{
				"id": 1, "name": "existing-priority-group", "channel_id": "openai", "connection_type": "api_key",
				"params": `{"base_url":"https://example.com"}`, "models": `[{"id":"gpt-4o"}]`,
				"weight_manual": 75, "enabled": true, "created_at_ms": 1, "updated_at_ms": 1,
			}
			if err := db.Table("groups").Create(row).Error; err != nil {
				t.Fatal(err)
			}
			if interrupted {
				if err := migrations[28].Up(db); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := applyMigrations(db); err != nil {
					t.Fatal(err)
				}
			}
			var got struct {
				Name         string
				Priority     int
				WeightManual int
				Params       string
				Models       string
			}
			if err := db.Table("groups").Where("id = ?", 1).Take(&got).Error; err != nil {
				t.Fatal(err)
			}
			if got.Priority != 0 || got.WeightManual != 75 || got.Name != row["name"] || got.Params == "" || got.Models == "" {
				t.Fatalf("migration changed existing data: %#v", got)
			}
		})
	}
}
