package test

import (
	"context"
	"math"
	"net/http"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

// karlorz fork patch: Poe 前缀模型在 v2 同步拉取中仍标记为 poe 风格，并按 Poe 缓存读取费率覆盖来源缓存价。
func TestFetchPricingSyncMarksPoePrefixedModelsAndOverridesCachePrices(t *testing.T) {
	transport := http.DefaultTransport
	http.DefaultTransport = pricingCatalogTransport{body: `{
		"deepseek": {
			"id": "deepseek",
			"name": "DeepSeek",
			"models": {
				"deepseek-v4-flash": {
					"id": "deepseek-v4-flash",
					"name": "DeepSeek V4 Flash",
					"family": "deepseek",
					"cost": {"input": 0.14, "output": 0.28, "cache_read": 0.0028, "cache_write": 1.23}
				}
			}
		}
	}`}
	t.Cleanup(func() { http.DefaultTransport = transport })

	db := openUsageServiceTestDatabase(t)
	if err := db.Create(&entities.UsageIdentity{
		Name: "poe-lite-dd", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey",
		Identity: "poe-lite-dd", Type: "openai", Provider: "poe", Prefix: "dd",
	}).Error; err != nil {
		t.Fatalf("seed poe identity: %v", err)
	}
	provider := newPricingTestProvider(t, db, emptyPricingCatalogForTest(), stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{
		{ID: "dd/deepseek-v4-flash"}, {ID: "deepseek-v4-flash"},
	}}}})
	preview, err := provider.FetchPricingSync(context.Background(), "models-dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Matches) != 2 {
		t.Fatalf("expected two matches, got %+v", preview)
	}
	for _, match := range preview.Matches {
		switch match.Model {
		case "dd/deepseek-v4-flash":
			if match.PricingStyle != entities.ModelPricingStylePoe || match.BasePrices.Input != 0.14 || match.BasePrices.Output != 0.28 ||
				math.Abs(match.BasePrices.CacheRead-0.028) > 1e-12 || match.BasePrices.CacheWrite != 0 {
				t.Fatalf("unexpected poe match: %+v", match)
			}
		case "deepseek-v4-flash":
			if match.PricingStyle != entities.ModelPricingStyleOpenAI || match.BasePrices.CacheRead != 0.0028 || match.BasePrices.CacheWrite != 1.23 {
				t.Fatalf("non-poe model must keep source cache prices: %+v", match)
			}
		default:
			t.Fatalf("unexpected match %+v", match)
		}
	}
}

// karlorz fork patch: 完整价格保存接受 poe 风格。
func TestSavePricingModelAcceptsPoeStyle(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, _ := newCatalogPricingService(t, db)
	config := completeServiceConfig("dd/deepseek-v4-flash", 0.14)
	config.PricingStyle = entities.ModelPricingStylePoe
	if _, err := provider.SavePricingModel(context.Background(), config); err != nil {
		t.Fatalf("save poe pricing: %v", err)
	}
	listed, err := provider.ListPricingModels(context.Background())
	if err != nil || len(listed.Models) != 1 || listed.Models[0].PricingStyle != entities.ModelPricingStylePoe {
		t.Fatalf("poe style not persisted: %+v err=%v", listed, err)
	}
	apply := servicedto.PricingSyncApplyRequest{Source: "models-dev", Items: []servicedto.PricingSyncApplyItem{{
		Model: "dd/new-poe-model", BasePrices: pricing.BasePrices{Input: 1, CacheRead: 0.2}, PricingStyle: entities.ModelPricingStylePoe,
	}}}
	if _, err := provider.ApplyPricingSync(context.Background(), apply); err != nil {
		t.Fatalf("apply poe sync item: %v", err)
	}
}
