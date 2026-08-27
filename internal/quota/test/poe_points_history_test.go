package test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/timeutil"
)

func TestPoePointsHistoryPayloadParserTolerant(t *testing.T) {
	// Mixed payload with valid, malformed, nested, and snake/camel variations
	bodyText := `{
		"has_more": true,
		"next_starting": "cursor_123",
		"data": [
			{
				"query_id": "qid_1",
				"bot_name": "Claude-3.5-Sonnet",
				"cost_points": 25.5,
				"cost_usd": "0.0051",
				"cost_breakdown_in_points": 25.5,
				"creation_time": 1787788800000000
			},
			{
				"query_id": "qid_2",
				"botName": "GPT-4o",
				"costPoints": "15.0",
				"costUsd": 0.003,
				"points_breakdown": 15.0,
				"created_at": 1787789000
			},
			{
				"query_id": "",
				"bot_name": "invalid_empty_id",
				"cost_points": 10
			},
			{
				"bad_entry": true
			},
			{
				"id": "qid_3",
				"model": "Gemini-1.5-Pro",
				"points_cost": 50.0,
				"usd_cost": 0.01,
				"creationTime": 1787790000000000
			}
		]
	}`

	resp := &apicall.Response{
		StatusCode: 200,
		BodyText:   bodyText,
		Body:       json.RawMessage(bodyText),
	}

	payload, err := quota.ParsePoePointsHistoryPayloadForTest(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !payload.HasMore {
		t.Fatal("expected has_more to be true")
	}
	if payload.NextStarting != "cursor_123" {
		t.Fatalf("expected next_starting 'cursor_123', got %q", payload.NextStarting)
	}
	if len(payload.Data) != 3 {
		t.Fatalf("expected 3 valid parsed entries, got %d", len(payload.Data))
	}

	// Item 1
	item1 := payload.Data[0]
	if item1.QueryID != "qid_1" || item1.BotName != "Claude-3.5-Sonnet" {
		t.Fatalf("unexpected item1: %+v", item1)
	}
	if item1.CostPoints == nil || *item1.CostPoints != 25.5 {
		t.Fatalf("unexpected item1 cost_points: %v", item1.CostPoints)
	}
	if item1.CostUSD == nil || *item1.CostUSD != 0.0051 {
		t.Fatalf("unexpected item1 cost_usd: %v", item1.CostUSD)
	}

	// Item 2
	item2 := payload.Data[1]
	if item2.QueryID != "qid_2" || item2.BotName != "GPT-4o" {
		t.Fatalf("unexpected item2: %+v", item2)
	}
	if item2.CostPoints == nil || *item2.CostPoints != 15.0 {
		t.Fatalf("unexpected item2 cost_points: %v", item2.CostPoints)
	}

	// Item 3
	item3 := payload.Data[2]
	if item3.QueryID != "qid_3" || item3.BotName != "Gemini-1.5-Pro" {
		t.Fatalf("unexpected item3: %+v", item3)
	}
	if item3.CostPoints == nil || *item3.CostPoints != 50.0 {
		t.Fatalf("unexpected item3 cost_points: %v", item3.CostPoints)
	}
}

func TestUpsertPoePointsHistoryIdempotent(t *testing.T) {
	db := openQuotaTestDatabase(t)
	if err := db.AutoMigrate(&entities.PoePointsHistory{}); err != nil {
		t.Fatalf("AutoMigrate PoePointsHistory returned error: %v", err)
	}

	now := timeutil.NormalizeStorageTime(time.Now())
	firstSeen := now.Add(-10 * time.Minute)
	lastSeenInitial := now.Add(-5 * time.Minute)
	lastSeenUpdated := now

	costPoints := 25.5
	costUSD := 0.0051
	initialRecord := entities.PoePointsHistory{
		QueryID:               "qid_test_idempotent",
		AuthIndex:             "poe_test_auth",
		BotName:               "Claude-3.5-Sonnet",
		CostPoints:            &costPoints,
		CostUSD:               &costUSD,
		CostBreakdownInPoints: &costPoints,
		ObservedAt:            firstSeen,
		FirstSeenAt:           firstSeen,
		LastSeenAt:            lastSeenInitial,
	}

	ctx := context.Background()

	// Initial insert
	err := quota.UpsertPoePointsHistory(ctx, db, []entities.PoePointsHistory{initialRecord})
	if err != nil {
		t.Fatalf("initial upsert failed: %v", err)
	}

	// Verify row count is 1
	var count int64
	db.Model(&entities.PoePointsHistory{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}

	var stored entities.PoePointsHistory
	if err := db.First(&stored, "query_id = ?", "qid_test_idempotent").Error; err != nil {
		t.Fatalf("failed to fetch stored record: %v", err)
	}
	if !stored.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("expected FirstSeenAt %v, got %v", firstSeen, stored.FirstSeenAt)
	}
	if !stored.LastSeenAt.Equal(lastSeenInitial) {
		t.Fatalf("expected LastSeenAt %v, got %v", lastSeenInitial, stored.LastSeenAt)
	}

	// Re-insert same query_id with updated LastSeenAt and different initial timestamps
	reinsertRecord := entities.PoePointsHistory{
		QueryID:               "qid_test_idempotent",
		AuthIndex:             "poe_test_auth",
		BotName:               "Claude-3.5-Sonnet",
		CostPoints:            &costPoints,
		CostUSD:               &costUSD,
		CostBreakdownInPoints: &costPoints,
		ObservedAt:            now,
		FirstSeenAt:           now, // should not overwrite initial FirstSeenAt
		LastSeenAt:            lastSeenUpdated,
	}

	err = quota.UpsertPoePointsHistory(ctx, db, []entities.PoePointsHistory{reinsertRecord})
	if err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	// Row count must remain 1
	db.Model(&entities.PoePointsHistory{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected row count to stay 1, got %d", count)
	}

	var updated entities.PoePointsHistory
	if err := db.First(&updated, "query_id = ?", "qid_test_idempotent").Error; err != nil {
		t.Fatalf("failed to fetch updated record: %v", err)
	}

	// FirstSeenAt preserved, LastSeenAt updated
	if !updated.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("expected FirstSeenAt to stay %v, got %v", firstSeen, updated.FirstSeenAt)
	}
	if !updated.LastSeenAt.Equal(lastSeenUpdated) {
		t.Fatalf("expected LastSeenAt to update to %v, got %v", lastSeenUpdated, updated.LastSeenAt)
	}
}

func TestPoePointsHistoryRunnerPollsAndUpserts(t *testing.T) {
	db := openQuotaTestDatabase(t)
	if err := db.AutoMigrate(entities.All()...); err != nil {
		t.Fatalf("AutoMigrate returned error: %v", err)
	}

	// Seed an active Poe identity
	seedUsageIdentity(t, db, entities.UsageIdentity{
		Identity: "poe_key_123",
		Provider: "poe",
		Type:     "openai",
		AuthType: entities.UsageIdentityAuthTypeAIProvider,
		Name:     "Poe Main Key",
	})

	bodyText := `{
		"has_more": false,
		"data": [
			{
				"query_id": "query_abc_1",
				"bot_name": "Claude-3.5-Sonnet",
				"cost_points": 12.0,
				"cost_usd": 0.0024,
				"cost_breakdown_in_points": 12.0,
				"creation_time": 1787788800000000
			},
			{
				"query_id": "query_abc_2",
				"bot_name": "GPT-4o",
				"cost_points": 8.5,
				"cost_usd": 0.0017,
				"cost_breakdown_in_points": 8.5,
				"creation_time": 1787788900000000
			}
		]
	}`

	caller := &staticResponseManagementCaller{response: &apicall.Response{
		StatusCode: 200,
		BodyText:   bodyText,
		Body:       json.RawMessage(bodyText),
	}}

	service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{
		PricingCatalog:               emptyPricingCatalogForTest(),
		PoePointsHistoryPollInterval: 10 * time.Millisecond,
	})
	defer service.StopRefreshTasks()

	// Trigger immediate poll
	service.TriggerPoePointsHistoryPoll()

	// Wait for records to be upserted
	var records []entities.PoePointsHistory
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		db.Find(&records)
		if len(records) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records upserted in poe_points_history, got %d", len(records))
	}

	// Re-trigger poll to verify idempotency through runner
	service.TriggerPoePointsHistoryPoll()
	time.Sleep(100 * time.Millisecond)

	var count int64
	db.Model(&entities.PoePointsHistory{}).Count(&count)
	if count != 2 {
		t.Fatalf("expected count to remain 2 after second poll, got %d", count)
	}
}
