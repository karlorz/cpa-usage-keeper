package test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

func TestBuildSpendDashboardAggregatesUSDAndPoePoints(t *testing.T) {
	db := openTestDatabase(t)
	if err := db.AutoMigrate(&entities.UsageOverviewDailyStat{}, &entities.PoePointsHistory{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	now := timeutil.NormalizeStorageTime(time.Date(2026, 8, 27, 12, 0, 0, 0, time.Local))
	day1 := time.Date(2026, 8, 26, 0, 0, 0, 0, time.Local)
	day2 := time.Date(2026, 8, 27, 0, 0, 0, 0, time.Local)

	// v2: 消费看板读取日桶已存 cost_usd，不再按当前价格重算。
	storedCost1 := 20.0
	storedCost2 := 10.0

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
			CostUSD:      &storedCost1,
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
			CostUSD:      &storedCost2,
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

	start := day1
	end := now
	dashboard, err := repository.BuildSpendDashboard(context.Background(), db, repodto.UsageQueryFilter{
		StartTime: &start,
		EndTime:   &end,
	})
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
	db := openTestDatabase(t)
	if err := db.AutoMigrate(&entities.UsageOverviewDailyStat{}, &entities.PoePointsHistory{}); err != nil {
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

	start := day
	end := day.Add(24 * time.Hour)

	dashboard, err := repository.BuildSpendDashboard(context.Background(), db, repodto.UsageQueryFilter{
		StartTime: &start,
		EndTime:   &end,
		AuthIndex: "auth-a",
	})
	if err != nil {
		t.Fatalf("BuildSpendDashboard: %v", err)
	}

	if len(dashboard.Rows) != 1 || dashboard.Rows[0].AuthIndex != "auth-a" {
		t.Fatalf("expected 1 row for auth-a, got %+v", dashboard.Rows)
	}
}

func TestBuildSpendDashboardHonorsExclusiveCustomEnd(t *testing.T) {
	db := openTestDatabase(t)
	if err := db.AutoMigrate(&entities.UsageOverviewDailyStat{}, &entities.PoePointsHistory{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	day := time.Date(2026, 10, 17, 0, 0, 0, 0, time.Local)
	next := day.AddDate(0, 0, 1)
	points := 10.0
	cost := 1.0
	for index, at := range []time.Time{day, next} {
		if err := db.Create(&entities.UsageOverviewDailyStat{BucketStart: at, APIGroupKey: "key-1", Model: "model-a", AuthIndex: "auth-a", RequestCount: 1, CostUSD: &cost}).Error; err != nil {
			t.Fatalf("seed daily stat: %v", err)
		}
		if err := db.Create(&entities.PoePointsHistory{QueryID: fmt.Sprintf("q-%d", index), AuthIndex: "auth-a", BotName: "bot-1", CostPoints: &points, ObservedAt: at.Add(time.Hour)}).Error; err != nil {
			t.Fatalf("seed poe history: %v", err)
		}
	}
	// 选择「仅 10 月 17 日」：解析器给出 [17 日 00:00, 18 日 00:00) 开区间。
	start, end := day, next
	dashboard, err := repository.BuildSpendDashboard(context.Background(), db, repodto.UsageQueryFilter{
		StartTime: &start, EndTime: &end, EndExclusive: true,
	})
	if err != nil {
		t.Fatalf("BuildSpendDashboard: %v", err)
	}
	for _, row := range dashboard.Rows {
		if row.Date != day.Format(time.DateOnly) {
			t.Fatalf("exclusive custom end leaked the following day: %+v", dashboard.Rows)
		}
	}
	if len(dashboard.Rows) == 0 {
		t.Fatal("expected spend rows for the selected day")
	}
}

// Issue: Poe-prefixed CPA model names must merge with the unprefixed Poe bot_name instead of double-counting USD.
func TestBuildSpendDashboardMergesPoePrefixedModelWithoutDoubleCountingUSD(t *testing.T) {
	db := openTestDatabase(t)
	if err := db.AutoMigrate(&entities.UsageOverviewDailyStat{}, &entities.PoePointsHistory{}, &entities.UsageIdentity{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	if err := db.Create(&entities.UsageIdentity{
		Name: "poe-dd", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey",
		Identity: "auth-poe", Type: "openai", Provider: "poe", Prefix: "dd",
	}).Error; err != nil {
		t.Fatalf("seed poe identity: %v", err)
	}
	day := time.Date(2026, 10, 17, 0, 0, 0, 0, time.Local)
	stored := 3.0
	unavailable := int64(0)
	if err := db.Create(&entities.UsageOverviewDailyStat{BucketStart: day, APIGroupKey: "key-1", Model: "dd/deepseek-v4-flash", AuthIndex: "auth-poe", RequestCount: 2, CostUSD: &stored, UnavailableCostCount: &unavailable}).Error; err != nil {
		t.Fatalf("seed daily stat: %v", err)
	}
	points1, usd1 := 1000.0, 2.0
	points2, usd2 := 500.0, 1.0
	for index, rec := range []entities.PoePointsHistory{
		{QueryID: "q-1", AuthIndex: "auth-poe", BotName: "DeepSeek-V4-Flash", CostPoints: &points1, CostUSD: &usd1, ObservedAt: day.Add(time.Hour)},
		{QueryID: "q-2", AuthIndex: "auth-poe", BotName: "deepseek-v4-flash", CostPoints: &points2, CostUSD: &usd2, ObservedAt: day.Add(2 * time.Hour)},
	} {
		if err := db.Create(&rec).Error; err != nil {
			t.Fatalf("seed poe history %d: %v", index, err)
		}
	}
	start, end := day, day.AddDate(0, 0, 1)
	dashboard, err := repository.BuildSpendDashboard(context.Background(), db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end, EndExclusive: true})
	if err != nil {
		t.Fatalf("BuildSpendDashboard: %v", err)
	}
	if len(dashboard.Rows) != 1 {
		t.Fatalf("expected prefixed usage and Poe rows to merge into one row, got %+v", dashboard.Rows)
	}
	row := dashboard.Rows[0]
	if row.Model != "dd/deepseek-v4-flash" || row.USDSpent != 3.0 || row.PointsSpent != 1500 {
		t.Fatalf("expected stored $3 (no Poe USD added) and 1500 points on the CPA model row, got %+v", row)
	}
	if dashboard.PoeHistoryExcluded {
		t.Fatal("poe history must be included without an API key filter")
	}

	// Model filter given as the CPA prefixed name still finds Poe history.
	filtered, err := repository.BuildSpendDashboard(context.Background(), db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end, EndExclusive: true, Model: "dd/deepseek-v4-flash"})
	if err != nil || len(filtered.Rows) != 1 || filtered.Rows[0].PointsSpent != 1500 {
		t.Fatalf("prefixed model filter lost Poe history: %+v err=%v", filtered, err)
	}
}

// Without a priced usage row, Poe-reported USD from every Poe record for that model/day is used (not only the first).
func TestBuildSpendDashboardFallsBackToAllPoeUSDWhenUsageUnpriced(t *testing.T) {
	db := openTestDatabase(t)
	if err := db.AutoMigrate(&entities.UsageOverviewDailyStat{}, &entities.PoePointsHistory{}, &entities.UsageIdentity{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	day := time.Date(2026, 10, 17, 0, 0, 0, 0, time.Local)
	zero := 0.0
	unavailable := int64(2)
	if err := db.Create(&entities.UsageOverviewDailyStat{BucketStart: day, APIGroupKey: "key-1", Model: "bot-1", AuthIndex: "auth-a", RequestCount: 2, CostUSD: &zero, UnavailableCostCount: &unavailable}).Error; err != nil {
		t.Fatalf("seed daily stat: %v", err)
	}
	usd1, usd2 := 0.25, 0.75
	for index, rec := range []entities.PoePointsHistory{
		{QueryID: "q-1", AuthIndex: "auth-a", BotName: "bot-1", CostUSD: &usd1, ObservedAt: day.Add(time.Hour)},
		{QueryID: "q-2", AuthIndex: "auth-a", BotName: "BOT-1", CostUSD: &usd2, ObservedAt: day.Add(2 * time.Hour)},
	} {
		if err := db.Create(&rec).Error; err != nil {
			t.Fatalf("seed poe history %d: %v", index, err)
		}
	}
	start, end := day, day.AddDate(0, 0, 1)
	dashboard, err := repository.BuildSpendDashboard(context.Background(), db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end, EndExclusive: true})
	if err != nil {
		t.Fatalf("BuildSpendDashboard: %v", err)
	}
	if len(dashboard.Rows) != 1 || dashboard.Rows[0].USDSpent != 1.0 {
		t.Fatalf("expected Poe USD fallback $1.00 for unpriced usage, got %+v", dashboard.Rows)
	}
}

// Issue: selecting an API key must not mix in account-wide Poe history (it has no API key attribution).
func TestBuildSpendDashboardExcludesPoeHistoryForAPIKeyFilter(t *testing.T) {
	db := openTestDatabase(t)
	if err := db.AutoMigrate(&entities.UsageOverviewDailyStat{}, &entities.PoePointsHistory{}, &entities.UsageIdentity{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	day := time.Date(2026, 10, 17, 0, 0, 0, 0, time.Local)
	cost1, cost2 := 1.0, 4.0
	for _, stat := range []entities.UsageOverviewDailyStat{
		{BucketStart: day, APIGroupKey: "key-1", Model: "model-a", AuthIndex: "auth-a", RequestCount: 1, CostUSD: &cost1},
		{BucketStart: day, APIGroupKey: "key-2", Model: "model-a", AuthIndex: "auth-a", RequestCount: 1, CostUSD: &cost2},
	} {
		if err := db.Create(&stat).Error; err != nil {
			t.Fatalf("seed daily stat: %v", err)
		}
	}
	points, usd := 900.0, 9.0
	if err := db.Create(&entities.PoePointsHistory{QueryID: "q-1", AuthIndex: "auth-a", BotName: "other-bot", CostPoints: &points, CostUSD: &usd, ObservedAt: day.Add(time.Hour)}).Error; err != nil {
		t.Fatalf("seed poe history: %v", err)
	}
	start, end := day, day.AddDate(0, 0, 1)
	dashboard, err := repository.BuildSpendDashboard(context.Background(), db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end, EndExclusive: true, APIGroupKey: "key-1"})
	if err != nil {
		t.Fatalf("BuildSpendDashboard: %v", err)
	}
	if !dashboard.PoeHistoryExcluded {
		t.Fatal("expected PoeHistoryExcluded for API key filter")
	}
	if len(dashboard.Rows) != 1 || dashboard.Rows[0].USDSpent != 1.0 || dashboard.Rows[0].PointsSpent != 0 {
		t.Fatalf("expected only key-1 stored USD and no Poe points, got %+v", dashboard.Rows)
	}
}

// Codex #34 review: Poe's full charge replaces (not adds to) a partial stored subtotal, and completeness is judged
// across every daily-stat row merged into the dashboard row (here: two API keys, one incomplete).
func TestBuildSpendDashboardPoeFallbackReplacesIncompleteMergedUsage(t *testing.T) {
	db := openTestDatabase(t)
	if err := db.AutoMigrate(&entities.UsageOverviewDailyStat{}, &entities.PoePointsHistory{}, &entities.UsageIdentity{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	deletedAt := time.Date(2026, 10, 18, 0, 0, 0, 0, time.Local)
	// 已删除身份、冒号前缀：历史仍需对齐。
	if err := db.Create(&entities.UsageIdentity{
		Name: "poe-old", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey",
		Identity: "auth-poe", Type: "openai", Provider: "Poe", Prefix: "PX", IsDeleted: true, DeletedAt: &deletedAt,
	}).Error; err != nil {
		t.Fatalf("seed poe identity: %v", err)
	}
	day := time.Date(2026, 10, 17, 0, 0, 0, 0, time.Local)
	priced, partial := 1.0, 0.25
	zero, one := int64(0), int64(1)
	for _, stat := range []entities.UsageOverviewDailyStat{
		{BucketStart: day, APIGroupKey: "key-a", Model: "px:bot-1", AuthIndex: "auth-poe", RequestCount: 2, CostUSD: &priced, UnavailableCostCount: &zero},
		{BucketStart: day, APIGroupKey: "key-b", Model: "px:bot-1", AuthIndex: "auth-poe", RequestCount: 2, CostUSD: &partial, UnavailableCostCount: &one},
	} {
		if err := db.Create(&stat).Error; err != nil {
			t.Fatalf("seed daily stat: %v", err)
		}
	}
	usd1, usd2 := 2.0, 1.0
	for index, rec := range []entities.PoePointsHistory{
		{QueryID: "q-1", AuthIndex: "auth-poe", BotName: "Bot-1", CostUSD: &usd1, ObservedAt: day.Add(time.Hour)},
		{QueryID: "q-2", AuthIndex: "auth-poe", BotName: "bot-1", CostUSD: &usd2, ObservedAt: day.Add(2 * time.Hour)},
	} {
		if err := db.Create(&rec).Error; err != nil {
			t.Fatalf("seed poe history %d: %v", index, err)
		}
	}
	start, end := day, day.AddDate(0, 0, 1)
	dashboard, err := repository.BuildSpendDashboard(context.Background(), db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end, EndExclusive: true})
	if err != nil {
		t.Fatalf("BuildSpendDashboard: %v", err)
	}
	if len(dashboard.Rows) != 1 {
		t.Fatalf("expected one merged row, got %+v", dashboard.Rows)
	}
	if got := dashboard.Rows[0].USDSpent; got != 3.0 {
		t.Fatalf("expected Poe full charge $3.00 (both Poe groups, replacing $1.25 incomplete stored), got %v", got)
	}

	// 选中单个完整计价的 API Key 时不合并 Poe，保留已存费用。
	scoped, err := repository.BuildSpendDashboard(context.Background(), db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end, EndExclusive: true, APIGroupKey: "key-a"})
	if err != nil || len(scoped.Rows) != 1 || scoped.Rows[0].USDSpent != 1.0 || !scoped.PoeHistoryExcluded {
		t.Fatalf("expected key-a stored $1.00 with Poe excluded, got %+v err=%v", scoped, err)
	}
}
