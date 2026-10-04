package migrations

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ID0030 = "0030_group_priority_range"

// maxGroupPriority0030 mirrors state.MaxPriority. The bound is duplicated here
// so the clamp stays valid even if the runtime constant moves again.
const maxGroupPriority0030 = 100

// Up0030 clamps priorities that were stored while the accepted range was
// unbounded. Without it, an upgrade would fail runtime snapshot validation at
// startup for groups such as priority 999. Values above the cap collapse onto
// the cap, which preserves their relative ordering against lower tiers.
func Up0030(db *gorm.DB) error {
	if err := ValidateRecoverable0030(db); err != nil {
		return err
	}
	if db.Migrator().HasColumn("groups", "priority") {
		if err := db.Exec(
			"UPDATE ? SET priority = ? WHERE priority > ?",
			clause.Table{Name: "groups"}, maxGroupPriority0030, maxGroupPriority0030,
		).Error; err != nil {
			return fmt.Errorf("clamp high group priority: %w", err)
		}
		if err := db.Exec(
			"UPDATE ? SET priority = 0 WHERE priority < 0",
			clause.Table{Name: "groups"},
		).Error; err != nil {
			return fmt.Errorf("clamp low group priority: %w", err)
		}
	}
	return Validate0030(db)
}

// ValidateRecoverable0030 deliberately does not assert the clamped range, so it
// accepts the state before and after the update. Asserting the post-condition
// here would make the migration reject the very rows it exists to fix.
func ValidateRecoverable0030(db *gorm.DB) error {
	switch db.Dialector.Name() {
	case "sqlite", "mysql", "postgres":
	default:
		return fmt.Errorf("unsupported group priority range migration driver %q", db.Dialector.Name())
	}
	if !db.Migrator().HasTable("groups") {
		return fmt.Errorf("groups table is missing")
	}
	return nil
}

func Validate0030(db *gorm.DB) error {
	if !db.Migrator().HasColumn("groups", "priority") {
		return fmt.Errorf("group priority is missing")
	}
	var outOfRange int64
	if err := db.Table("groups").
		Where("priority < 0 OR priority > ?", maxGroupPriority0030).
		Count(&outOfRange).Error; err != nil {
		return fmt.Errorf("count out-of-range group priority: %w", err)
	}
	if outOfRange != 0 {
		return fmt.Errorf("group priority must be between 0 and %d", maxGroupPriority0030)
	}
	return nil
}
