package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/timeutil"
)

func TestSumPoePointsSpendByAuthIndex(t *testing.T) {
	db := openQuotaTestDB(t)
	if err := db.AutoMigrate(&entities.PoePointsHistory{}); err != nil {
		t.Fatalf("AutoMigrate PoePointsHistory: %v", err)
	}

	now := timeutil.NormalizeStorageTime(time.Date(2026, 8, 27, 12, 0, 0, 0, time.Local))
	cycleStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)

	cost1 := 50.0
	costUSD1 := 0.01
	cost2 := 25.5
	costUSD2 := 0.0051
	costOld := 100.0
	costUSDOld := 0.02
	costOther := 200.0
	costUSDOther := 0.04

	records := []entities.PoePointsHistory{
		{
			QueryID:     "q1",
			AuthIndex:   "target-auth",
			BotName:     "Claude-3.5-Sonnet",
			CostPoints:  &cost1,
			CostUSD:     &costUSD1,
			ObservedAt:  now.Add(-2 * time.Hour),
			FirstSeenAt: now.Add(-2 * time.Hour),
			LastSeenAt:  now.Add(-2 * time.Hour),
		},
		{
			QueryID:     "q2",
			AuthIndex:   "target-auth",
			BotName:     "GPT-4o",
			CostPoints:  &cost2,
			CostUSD:     &costUSD2,
			ObservedAt:  now.Add(-1 * time.Hour),
			FirstSeenAt: now.Add(-1 * time.Hour),
			LastSeenAt:  now.Add(-1 * time.Hour),
		},
		{
			QueryID:     "q_old",
			AuthIndex:   "target-auth",
			BotName:     "Claude-3.5-Sonnet",
			CostPoints:  &costOld,
			CostUSD:     &costUSDOld,
			ObservedAt:  cycleStart.Add(-24 * time.Hour),
			FirstSeenAt: cycleStart.Add(-24 * time.Hour),
			LastSeenAt:  cycleStart.Add(-24 * time.Hour),
		},
		{
			QueryID:     "q_other",
			AuthIndex:   "other-auth",
			BotName:     "GPT-4o",
			CostPoints:  &costOther,
			CostUSD:     &costUSDOther,
			ObservedAt:  now.Add(-1 * time.Hour),
			FirstSeenAt: now.Add(-1 * time.Hour),
			LastSeenAt:  now.Add(-1 * time.Hour),
		},
	}

	for _, rec := range records {
		if err := db.Create(&rec).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}

	stats, err := quota.SumPoePointsSpendByAuthIndex(context.Background(), db, "target-auth", cycleStart, nil)
	if err != nil {
		t.Fatalf("SumPoePointsSpendByAuthIndex error: %v", err)
	}

	expectedPoints := 75.5
	expectedUSD := 0.0151
	if stats.PointsSpent != expectedPoints {
		t.Fatalf("expected PointsSpent %.2f, got %.2f", expectedPoints, stats.PointsSpent)
	}
	if stats.USDSpent != expectedUSD {
		t.Fatalf("expected USDSpent %.4f, got %.4f", expectedUSD, stats.USDSpent)
	}
}

func TestAttachPoeSpendStatsEnrichesCheckResponse(t *testing.T) {
	db := openQuotaTestDB(t)
	if err := db.AutoMigrate(&entities.PoePointsHistory{}); err != nil {
		t.Fatalf("AutoMigrate PoePointsHistory: %v", err)
	}

	now := timeutil.NormalizeStorageTime(time.Date(2026, 8, 27, 12, 0, 0, 0, time.Local))
	costPoints := 1200.0
	costUSD := 0.24

	rec := entities.PoePointsHistory{
		QueryID:     "q_spend_1",
		AuthIndex:   "poe-spend-auth",
		BotName:     "Claude-3.5-Sonnet",
		CostPoints:  &costPoints,
		CostUSD:     &costUSD,
		ObservedAt:  now.Add(-10 * time.Minute),
		FirstSeenAt: now.Add(-10 * time.Minute),
		LastSeenAt:  now.Add(-10 * time.Minute),
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("seed spend record: %v", err)
	}

	seedUsageIdentity(t, db, entities.UsageIdentity{
		AuthType: entities.UsageIdentityAuthTypeAIProvider,
		Identity: "poe-spend-auth",
		Type:     "openai",
		Provider: "poe",
		Name:     "Poe Key",
	})

	balance := 10000.0
	handler := &recordingProviderHandler{
		output: quota.ProviderOutput{
			Provider: "poe",
			Result: quota.PoeResult{
				Usage: &quota.PoeUsagePayload{
					CurrentPointBalance: &balance,
				},
			},
		},
	}
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"poe": handler}))

	response, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "poe-spend-auth"})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}

	if len(response.Quota) != 3 {
		t.Fatalf("expected 3 quota rows (balance, points_spent_cycle, usd_spent_cycle), got %d: %+v", len(response.Quota), response.Quota)
	}

	var foundPointsSpent, foundUSDSpent bool
	for _, r := range response.Quota {
		if r.Key == "points_spent_cycle" {
			foundPointsSpent = true
			if r.Label != "Points Spent (Cycle)" || r.Scope != "billing" || r.Metric != "points" {
				t.Fatalf("unexpected points_spent_cycle row metadata: %+v", r)
			}
			if r.Used == nil || *r.Used != 1200.0 {
				t.Fatalf("expected points_spent_cycle used 1200.0, got %v", r.Used)
			}
		}
		if r.Key == "usd_spent_cycle" {
			foundUSDSpent = true
			if r.Label != "USD Spent (Cycle)" || r.Scope != "billing" || r.Metric != "usd_cents" {
				t.Fatalf("unexpected usd_spent_cycle row metadata: %+v", r)
			}
			if r.Used == nil || *r.Used != 24.0 { // 0.24 USD * 100 cents
				t.Fatalf("expected usd_spent_cycle used 24.0 cents, got %v", r.Used)
			}
		}
	}

	if !foundPointsSpent || !foundUSDSpent {
		t.Fatalf("expected spend rows not found: foundPoints=%v foundUSD=%v", foundPointsSpent, foundUSDSpent)
	}
}
