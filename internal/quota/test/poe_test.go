package test

import (
	"context"
	"encoding/json"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestPoeProviderCallsUsageRequest(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText: `{"current_point_balance":300,"plan_points_balance":300,"addon_point_balance":0,"plan_balance_usd":"0.0091","addon_balance_usd":"0.0000","total_balance_usd":"0.0091","points_cycle_start_time":1787788800000000,"next_daily_grant_time":1787788800000000,"next_monthly_grant_time":0,"next_daily_grant_amount":300,"next_monthly_grant_amount":0,"auto_recharge":null}`,
		Body:       json.RawMessage(`{"current_point_balance":300,"plan_points_balance":300,"addon_point_balance":0,"plan_balance_usd":"0.0091","addon_balance_usd":"0.0000","total_balance_usd":"0.0091","points_cycle_start_time":1787788800000000,"next_daily_grant_time":1787788800000000,"next_monthly_grant_time":0,"next_daily_grant_amount":300,"next_monthly_grant_amount":0,"auto_recharge":null}`),
	}}}
	provider := quota.NewPoeProvider(caller, quota.DefaultProviderConfigs().Poe)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "poe-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if output.Provider != "poe" {
		t.Fatalf("expected poe output provider, got %q", output.Provider)
	}
	result, ok := output.Result.(quota.PoeResult)
	if !ok {
		t.Fatalf("expected poe result type, got %T", output.Result)
	}
	if result.Usage == nil || result.Usage.CurrentPointBalance == nil || *result.Usage.CurrentPointBalance != 300 || result.Usage.NextDailyGrantTime == nil || *result.Usage.NextDailyGrantTime != 1787788800000000 {
		t.Fatalf("expected parsed poe usage payload, got %#v", result.Usage)
	}
	encoded, err := json.Marshal(output.Result)
	if err != nil {
		t.Fatalf("marshal poe result: %v", err)
	}
	body := string(encoded)
	if !contains(body, `"usage":{"current_point_balance":300`) || contains(body, "bodyText") || contains(body, "statusCode") {
		t.Fatalf("unexpected poe result JSON: %s", body)
	}
	if len(caller.requests) != 1 {
		t.Fatalf("expected one api-call request, got %d", len(caller.requests))
	}
	request := caller.requests[0]
	if request.AuthIndex != "poe-auth" || request.Method != "GET" || request.URL != "https://api.poe.com/usage/current_balance" {
		t.Fatalf("unexpected api-call request: %+v", request)
	}
	if request.Header["Authorization"] != "Bearer $TOKEN$" || request.Header["Accept"] != "application/json" {
		t.Fatalf("unexpected api-call headers: %+v", request.Header)
	}
	if request.Data != nil {
		t.Fatalf("expected no data body, got %#v", request.Data)
	}
}

func TestPoeProviderNormalizesNumberForwardQuotaRows(t *testing.T) {
	body := json.RawMessage(`{"current_point_balance":300,"plan_points_balance":280,"addon_point_balance":20,"plan_balance_usd":"0.0091","addon_balance_usd":"0.0006","total_balance_usd":"0.0097","points_cycle_start_time":1787788800000000,"next_daily_grant_time":1787868000000000,"next_daily_grant_amount":300,"auto_recharge":null}`)
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   string(body),
		Body:       body,
	}}}
	provider := quota.NewPoeProvider(caller, quota.DefaultProviderConfigs().Poe)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "poe-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	rows := quota.NormalizeQuotaRows(output)
	if len(rows) != 5 {
		t.Fatalf("expected five poe quota rows, got %#v", rows)
	}

	balance := findQuotaRow(t, rows, "current_point_balance")
	assertQuotaText(t, balance, "Compute Points", "billing", "points")
	assertFloatField(t, balance.Remaining, 300, "compute points balance")

	plan := findQuotaRow(t, rows, "plan_points_balance")
	assertFloatField(t, plan.Remaining, 280, "plan points balance")

	addon := findQuotaRow(t, rows, "addon_point_balance")
	assertFloatField(t, addon.Remaining, 20, "addon points balance")

	usd := findQuotaRow(t, rows, "total_balance_usd")
	assertQuotaText(t, usd, "USD Equivalent", "billing", "usd_cents")
	assertFloatField(t, usd.Used, 0.97, "usd equivalent cents")

	grant := findQuotaRow(t, rows, "next_daily_grant")
	assertQuotaText(t, grant, "Next Daily Grant", "billing", "points")
	assertFloatField(t, grant.Remaining, 300, "next daily grant amount")
	if grant.ResetAt == "" {
		t.Fatalf("expected next daily grant resetAt, got %#v", grant)
	}
}

func TestPoeProviderSkipsZeroAddonAndMissingGrant(t *testing.T) {
	body := json.RawMessage(`{"current_point_balance":300,"plan_points_balance":300,"addon_point_balance":0,"plan_balance_usd":"0.0091","total_balance_usd":"0.0091","points_cycle_start_time":1787788800000000,"next_daily_grant_time":0,"next_daily_grant_amount":300}`)
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   string(body),
		Body:       body,
	}}}
	provider := quota.NewPoeProvider(caller, quota.DefaultProviderConfigs().Poe)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "poe-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	rows := quota.NormalizeQuotaRows(output)
	if len(rows) != 3 {
		t.Fatalf("expected three poe quota rows (no add-on, no grant row), got %#v", rows)
	}
	for _, row := range rows {
		if row.Key == "addon_point_balance" || row.Key == "next_daily_grant" {
			t.Fatalf("expected addon/grant rows to be omitted, got %#v", row)
		}
	}
}

func TestPoeProviderRejectsEmptyUsageResponse(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   `{}`,
		Body:       json.RawMessage(`{}`),
	}}}
	provider := quota.NewPoeProvider(caller, quota.DefaultProviderConfigs().Poe)

	_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "poe-auth"}})
	if err == nil {
		t.Fatal("expected error for empty poe usage response")
	}
}
