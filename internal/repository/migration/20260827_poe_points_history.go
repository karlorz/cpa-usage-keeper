package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func createPoePointsHistoryMigration(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&entities.PoePointsHistory{}); err != nil {
		return fmt.Errorf("create poe points history schema: %w", err)
	}
	return nil
}
