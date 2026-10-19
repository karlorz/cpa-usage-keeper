package test

import (
	"context"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
)

// karlorz fork patch: v1 fork 数据库可能保存 pricing_style='poe'；v1->v2 单向迁移读取旧价格时不能因此失败。
func TestLoadPublishedPricingConfigsAcceptsForkPoeStyle(t *testing.T) {
	db := openPublishedPricingFixture(t)
	if err := db.Exec(`INSERT INTO model_price_settings VALUES
		(1, 'dd/deepseek-v4-flash', 'poe', 0.14, 0.28, 0.028, 0, 1)`).Error; err != nil {
		t.Fatal(err)
	}
	configs, err := repository.LoadPublishedPricingConfigs(context.Background(), db)
	if err != nil || len(configs) != 1 {
		t.Fatalf("poe-styled legacy price must load: configs=%+v err=%v", configs, err)
	}
	snapshot, err := pricing.CompilePricingSnapshot(configs, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	model, ok := snapshot.PricingModelConfig("dd/deepseek-v4-flash")
	if !ok || model.PricingStyle != entities.ModelPricingStylePoe {
		t.Fatalf("poe style was not preserved: %+v", model)
	}
	// Poe 回执：114413 输入字符（含 113152 缓存读取）、89 输出 → $0.0034。
	fee := pricing.NewCatalog(snapshot).NewResolver().CalculateFee(repository.UsageEventCostSubject(entities.UsageEvent{
		Model: "dd/deepseek-v4-flash", InputTokens: 114413, OutputTokens: 89, CacheReadTokens: 113152,
	}))
	if !fee.Available || math.Abs(fee.TotalCostUSD-0.0034) > 0.00005 {
		t.Fatalf("poe fee=%+v, want ~0.0034", fee)
	}
}
