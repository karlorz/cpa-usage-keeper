package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

func TestBuildSpendDashboardAggregatesUSDAndPoePoints(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "spend-test.db")
	if err := db.AutoMigrate(&entities.UsageOverviewDailyStat{}, &entities.PoePointsHistory{}, &entities.ModelPriceSetting{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	now := timeutil.NormalizeStorageTime(time.Date(2026, 8, 27, 12, 0, 0, 0, time.Local))
	day1 := time.Date(2026, 8, 26, 0, 0, 0, 0, time.Local)
	day2 := time.Date(2026, 8, 27, 0, 0, 0, 0, time.Local)

	// Set up pricing for model-a
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                "model-a",
		PromptPricePer1M:     10.0,
		CompletionPricePer1M: 20.0,
	}); err != nil {
		t.Fatalf("seed pricing: %v", err)
	}

	// 1. Seed UsageOverviewDailyStat rows (contributes USD spent)
	dailyStats := []entities.UsageOverviewDailyStat{
		{
			BucketStart:  day1,
			APIGroupKey:  "key-1",
			Model:        "model-a",
			AuthIndex:    "auth-user-1",
			InputTokens:  1_000_000, // 10 USD
			OutputTokens: 500_000,   // 10 USD -> Total 20 USD
			TotalTokens:  1_500_000,
			RequestCount: 10,
		},
		{
			BucketStart:  day2,
			APIGroupKey:  "key-1",
			Model:        "model-a",
			AuthIndex:    "auth-user-1",
			InputTokens:  500_000, // 5 USD
			OutputTokens: 250_000, // 5 USD -> Total 10 USD
			TotalTokens:  750_000,
			RequestCount: 5,
		},
	}
	for _, stat := range dailyStats {
		if err := db.Create(&stat).Error; err != nil {
			t.Fatalf("seed daily stat: %v", err)
		}
	}

	// 2. Seed PoePointsHistory rows (contributes compute points)
	costPoints1 := 1500.0
	costUSD1 := 0.30
	costPoints2 := 2500.0
	costUSD2 := 0.50

	poeRecords := []entities.PoePointsHistory{
		{
			QueryID:     "q-1",
			AuthIndex:   "auth-user-1",
			BotName:     "model-a",
			CostPoints:  &costPoints1,
			CostUSD:     &costUSD1,
			ObservedAt:  day1.Add(10 * time.Hour),
			FirstSeenAt: day1.Add(10 * time.Hour),
			LastSeenAt:  day1.Add(10 * time.Hour),
		},
		{
			QueryID:     "q-2",
			AuthIndex:   "auth-user-1",
			BotName:     "deepseek-v4-flash",
			CostPoints:  &costPoints2,
			CostUSD:     &costUSD2,
			ObservedAt:  day2.Add(15 * time.Hour),
			FirstSeenAt: day2.Add(15 * time.Hour),
			LastSeenAt:  day2.Add(15 * time.Hour),
		},
	}
	for _, rec := range poeRecords {
		if err := db.Create(&rec).Error; err != nil {
			t.Fatalf("seed poe points history: %v", err)
		}
	}

	resolver := newUsageCostResolverForTest(t, db)

	start := day1
	end := now
	dashboard, err := repository.BuildSpendDashboard(context.Background(), db, repodto.UsageQueryFilter{
		StartTime: &start,
		EndTime:   &end,
	}, resolver)
	if err != nil {
		t.Fatalf("BuildSpendDashboard error: %v", err)
	}

	if len(dashboard.Rows) != 3 {
		t.Fatalf("expected 3 spend rows, got %d: %+v", len(dashboard.Rows), dashboard.Rows)
	}

	// Verify rows are grouped by auth_index, model, date
	for _, row := range dashboard.Rows {
		if row.Date == "2026-08-26" && row.Model == "model-a" && row.AuthIndex == "auth-user-1" {
			if row.USDSpent != 20.0 {
				t.Fatalf("expected USDSpent 20.0, got %f", row.USDSpent)
			}
			if row.PointsSpent != 1500.0 {
				t.Fatalf("expected PointsSpent 1500.0, got %f", row.PointsSpent)
			}
		}
		if row.Date == "2026-08-27" && row.Model == "deepseek-v4-flash" && row.AuthIndex == "auth-user-1" {
			if row.USDSpent != 0.50 {
				t.Fatalf("expected USDSpent 0.50, got %f", row.USDSpent)
			}
			if row.PointsSpent != 2500.0 {
				t.Fatalf("expected PointsSpent 2500.0, got %f", row.PointsSpent)
			}
		}
		if row.Date == "2026-08-27" && row.Model == "model-a" && row.AuthIndex == "auth-user-1" {
			if row.USDSpent != 10.0 {
				t.Fatalf("expected USDSpent 10.0, got %f", row.USDSpent)
			}
			if row.PointsSpent != 0.0 {
				t.Fatalf("expected PointsSpent 0.0, got %f", row.PointsSpent)
			}
		}
	}
}

func TestBuildSpendDashboardFiltersByAuthIndexAndModel(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "spend-filter-test.db")
	if err := db.AutoMigrate(&entities.UsageOverviewDailyStat{}, &entities.PoePointsHistory{}, &entities.ModelPriceSetting{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	day := time.Date(2026, 8, 27, 0, 0, 0, 0, time.Local)
	points := 100.0
	db.Create(&entities.PoePointsHistory{
		QueryID:    "q-1",
		AuthIndex:  "auth-a",
		BotName:    "bot-1",
		CostPoints: &points,
		ObservedAt: day.Add(time.Hour),
	})
	db.Create(&entities.PoePointsHistory{
		QueryID:    "q-2",
		AuthIndex:  "auth-b",
		BotName:    "bot-2",
		CostPoints: &points,
		ObservedAt: day.Add(time.Hour),
	})

	resolver := newUsageCostResolverForTest(t, db)
	start := day
	end := day.Add(24 * time.Hour)

	dashboard, err := repository.BuildSpendDashboard(context.Background(), db, repodto.UsageQueryFilter{
		StartTime: &start,
		EndTime:   &end,
		AuthIndex: "auth-a",
	}, resolver)
	if err != nil {
		t.Fatalf("BuildSpendDashboard: %v", err)
	}

	if len(dashboard.Rows) != 1 || dashboard.Rows[0].AuthIndex != "auth-a" {
		t.Fatalf("expected 1 row for auth-a, got %+v", dashboard.Rows)
	}
}
