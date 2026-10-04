package storage

import (
	"fmt"
	"os"
	"testing"

	"gorm.io/gorm"
)

func TestGroupPriorityRangeMigrationClampsExistingValues(t *testing.T) {
	testGroupPriorityRangeMigration(t, openInternalMigrationTestDatabase)
}

func TestExternalGroupPriorityRangeMigrationClampsExistingValues(t *testing.T) {
	dsn := os.Getenv("GPT_LOAD_DATABASE_TEST_DSN")
	if dsn == "" {
		t.Skip("GPT_LOAD_DATABASE_TEST_DSN is not set")
	}
	testGroupPriorityRangeMigration(t, func(t *testing.T) *gorm.DB {
		return openExternalIncrementalMigrationDatabase(t, dsn)
	})
}

func testGroupPriorityRangeMigration(t *testing.T, open func(*testing.T) *gorm.DB) {
	db := open(t)
	if err := applyMigrationRegistry(db, migrations[:29]); err != nil {
		t.Fatal(err)
	}
	// Priorities stored while the accepted range was 0-1000000, plus the
	// in-range values that must survive untouched.
	seeded := map[int64]int64{1: 0, 2: 50, 3: 100, 4: 999, 5: 1000000, 6: -5}
	for id, priority := range seeded {
		// groups.name carries a unique index, so every seeded row needs its own name.
		row := map[string]any{
			"id": id, "name": fmt.Sprintf("priority-range-group-%d", id), "channel_id": "openai", "connection_type": "api_key",
			"params": `{"base_url":"https://example.com"}`, "models": `[{"id":"gpt-4o"}]`,
			"weight_manual": 75, "priority": priority, "enabled": true, "created_at_ms": 1, "updated_at_ms": 1,
		}
		if err := db.Table("groups").Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := applyMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var rows []struct {
		ID           int64
		Priority     int
		WeightManual int
		Name         string
	}
	if err := db.Table("groups").Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	want := map[int64]int{1: 0, 2: 50, 3: 100, 4: 100, 5: 100, 6: 0}
	if len(rows) != len(want) {
		t.Fatalf("group count = %d, want %d", len(rows), len(want))
	}
	for _, row := range rows {
		if row.Priority != want[row.ID] {
			t.Fatalf("group %d priority = %d, want %d", row.ID, row.Priority, want[row.ID])
		}
		if row.WeightManual != 75 || row.Name != fmt.Sprintf("priority-range-group-%d", row.ID) {
			t.Fatalf("migration changed unrelated data: %#v", row)
		}
	}
}
