package migrations

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ID0029 = "0029_group_priority"

// Up0029 preserves the existing routing behavior by placing all existing groups
// in priority zero. The migration can resume after the column has been added.
func Up0029(db *gorm.DB) error {
	if err := ValidateRecoverable0029(db); err != nil {
		return err
	}
	if !db.Migrator().HasColumn("groups", "priority") {
		if err := db.Exec("ALTER TABLE ? ADD COLUMN priority INTEGER NOT NULL DEFAULT 0", clause.Table{Name: "groups"}).Error; err != nil {
			return fmt.Errorf("add group priority: %w", err)
		}
	}
	return Validate0029(db)
}

func ValidateRecoverable0029(db *gorm.DB) error {
	switch db.Dialector.Name() {
	case "sqlite", "mysql", "postgres":
	default:
		return fmt.Errorf("unsupported group priority migration driver %q", db.Dialector.Name())
	}
	if !db.Migrator().HasTable("groups") {
		return fmt.Errorf("groups table is missing")
	}
	if !db.Migrator().HasColumn("groups", "priority") {
		return nil
	}
	return Validate0029(db)
}

func Validate0029(db *gorm.DB) error {
	columns, err := db.Migrator().ColumnTypes("groups")
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() != "priority" {
			continue
		}
		if !strings.Contains(strings.ToLower(column.DatabaseTypeName()), "int") {
			return fmt.Errorf("group priority must be an integer")
		}
		if nullable, known := column.Nullable(); !known || nullable {
			return fmt.Errorf("group priority must not be nullable")
		}
		if value, known := column.DefaultValue(); !known || value != "0" {
			return fmt.Errorf("group priority default must be zero")
		}
		return nil
	}
	return fmt.Errorf("group priority is missing")
}
