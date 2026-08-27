package test

import (
	"path/filepath"
	"sort"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const poePointsHistoryMigrationVersion = "20260827_poe_points_history"

func TestPoePointsHistoryMigrationCreatesTableColumnsAndIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy-poe-points-history.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open existing database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	if err := db.Exec("CREATE TABLE legacy_sentinel (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create legacy sentinel: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", poePointsHistoryMigrationVersion).Delete(nil).Error; err != nil {
		t.Fatalf("make poe points history migration pending: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !db.Migrator().HasTable("poe_points_history") {
		t.Fatal("expected poe_points_history table")
	}

	for _, column := range []string{
		"query_id",
		"auth_index",
		"bot_name",
		"cost_points",
		"cost_usd",
		"cost_breakdown_in_points",
		"observed_at",
		"first_seen_at",
		"last_seen_at",
	} {
		if !db.Migrator().HasColumn(&entities.PoePointsHistory{}, column) {
			t.Fatalf("expected poe_points_history.%s column", column)
		}
	}

	// Verify indexes
	assertPoePointsHistoryIndexNames(t, db, "poe_points_history", []string{
		"idx_poe_points_history_auth_index",
		"idx_poe_points_history_bot_name",
		"idx_poe_points_history_observed_at",
	})

	// Verify idempotency / primary key constraint on query_id
	costPoints := 12.5
	costUSD := 0.0025
	breakdown := 12.5
	record := entities.PoePointsHistory{
		QueryID:               "poe-query-12345",
		AuthIndex:             "poe-account-1",
		BotName:               "gpt-4o",
		CostPoints:            &costPoints,
		CostUSD:               &costUSD,
		CostBreakdownInPoints: &breakdown,
		ObservedAt:            time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC),
		FirstSeenAt:           time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC),
		LastSeenAt:            time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC),
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatalf("insert valid poe points history record: %v", err)
	}

	duplicateRecord := record
	duplicateRecord.BotName = "claude-3-5-sonnet"
	if err := db.Create(&duplicateRecord).Error; err == nil {
		t.Fatal("expected duplicate query_id to be rejected by primary key constraint")
	}
}

func assertPoePointsHistoryIndexNames(t *testing.T, db *gorm.DB, table string, expected []string) {
	t.Helper()
	var rows []struct {
		Name string `gorm:"column:name"`
	}
	if err := db.Raw("PRAGMA index_list('" + table + "')").Scan(&rows).Error; err != nil {
		t.Fatalf("list %s indexes: %v", table, err)
	}
	actual := make([]string, 0, len(rows))
	for _, row := range rows {
		// SQLite auto-indexes for primary keys (sqlite_autoindex_...) can be ignored
		if row.Name != "" && !isPoePointsAutoIndex(row.Name) {
			actual = append(actual, row.Name)
		}
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if len(actual) != len(expected) {
		t.Fatalf("expected %s indexes %v, got %v", table, expected, actual)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("expected %s indexes %v, got %v", table, expected, actual)
		}
	}
}

func isPoePointsAutoIndex(name string) bool {
	return len(name) >= 17 && name[:17] == "sqlite_autoindex_"
}
