package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

type spendDashboardKey struct {
	authIndex string
	model     string
	date      string
}

// BuildSpendDashboard aggregates stored USD from UsageOverviewDailyStat and compute points from PoePointsHistory.
// v2: 费用直接读取日桶已存 cost_usd，不再按当前价格目录重算历史费用。
func BuildSpendDashboard(ctx context.Context, db *gorm.DB, filter dto.UsageQueryFilter) (*dto.SpendDashboardRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		return nil, fmt.Errorf("spend dashboard requires start_time and end_time")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	start := timeutil.NormalizeStorageTime(*filter.StartTime)
	end := timeutil.NormalizeStorageTime(*filter.EndTime)

	// Align to local calendar day boundaries
	windowStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())
	windowEnd := endDay.AddDate(0, 0, 1)
	// 自定义日期范围已由解析器转换为次日零点的开区间，不能再向后多扩一天。
	if filter.EndExclusive && end.Equal(endDay) {
		windowEnd = endDay
	}

	aggMap := make(map[spendDashboardKey]*dto.SpendRowRecord)
	// pricedUsage 记录该行是否已有可用的已存 USD；只有没有时才回退到 Poe 回报的 USD，避免重复计费。
	pricedUsage := make(map[spendDashboardKey]bool)

	// Poe 身份的 CPA 前缀（例如 dd/deepseek-v4-flash），用于把 CPA 模型名与 Poe bot_name 对齐。
	poePrefixes, err := loadPoeIdentityPrefixesByAuthIndex(db.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("load poe identity prefixes for spend dashboard: %w", err)
	}

	// 1. Load stored USD spend from UsageOverviewDailyStats
	dailyStats, err := loadDailyStatsForSpend(db.WithContext(ctx), filter, windowStart, windowEnd)
	if err != nil {
		return nil, fmt.Errorf("load daily stats for spend dashboard: %w", err)
	}

	for _, stat := range dailyStats {
		authIndex := strings.TrimSpace(stat.AuthIndex)
		model := strings.TrimSpace(stat.Model)
		date := timeutil.NormalizeStorageTime(stat.BucketStart).Format(time.DateOnly)

		costUSD := 0.0
		if stat.CostUSD != nil {
			costUSD = *stat.CostUSD
		}

		key := spendDashboardKey{
			authIndex: authIndex,
			model:     spendModelMatchKey(model, poePrefixes[authIndex]),
			date:      date,
		}

		entry, ok := aggMap[key]
		if !ok {
			entry = &dto.SpendRowRecord{
				AuthIndex: authIndex,
				Model:     model,
				Date:      date,
			}
			aggMap[key] = entry
		}
		entry.USDSpent += costUSD
		// 已存费用明确（cost_usd 非 NULL 且无不可计价请求）才视为已计价。
		if stat.CostUSD != nil && (stat.UnavailableCostCount == nil || *stat.UnavailableCostCount == 0) {
			pricedUsage[key] = true
		}
	}

	// 2. Load compute points and USD from poe_points_history, grouped in SQL.
	// poe_points_history 没有 API Key 归属；选中 API Key 时排除，避免把整个 Poe 账户的消费混入该 Key 的范围。
	if strings.TrimSpace(filter.APIGroupKey) != "" {
		return finalizeSpendDashboard(aggMap, true), nil
	}
	type poeSpendAggregate struct {
		AuthIndex   string  `gorm:"column:auth_index"`
		Model       string  `gorm:"column:bot_name"`
		Date        string  `gorm:"column:date"`
		PointsSpent float64 `gorm:"column:points_spent"`
		USDSpent    float64 `gorm:"column:usd_spent"`
	}
	var poeRows []poeSpendAggregate
	poeQuery := db.WithContext(ctx).Model(&entities.PoePointsHistory{}).
		Select("auth_index, bot_name, substr(observed_at, 1, 10) as date, COALESCE(SUM(cost_points), 0) as points_spent, COALESCE(SUM(cost_usd), 0) as usd_spent").
		Where("observed_at >= ? AND observed_at < ?", timeutil.FormatStorageTime(windowStart), timeutil.FormatStorageTime(windowEnd)).
		Group("auth_index, bot_name, substr(observed_at, 1, 10)")

	if authIndex := strings.TrimSpace(filter.AuthIndex); authIndex != "" {
		poeQuery = poeQuery.Where("auth_index = ?", authIndex)
	}

	if err := poeQuery.Scan(&poeRows).Error; err != nil {
		return nil, fmt.Errorf("load poe points history for spend dashboard: %w", err)
	}

	modelFilter := strings.TrimSpace(filter.Model)
	for _, p := range poeRows {
		authIndex := strings.TrimSpace(p.AuthIndex)
		model := strings.TrimSpace(p.Model)
		date := strings.TrimSpace(p.Date)
		prefix := poePrefixes[authIndex]
		matchModel := spendModelMatchKey(model, prefix)
		// 模型过滤按对齐后的身份比较，CPA 前缀模型名也能匹配到 Poe bot_name。
		if modelFilter != "" && spendModelMatchKey(modelFilter, prefix) != matchModel {
			continue
		}
		key := spendDashboardKey{
			authIndex: authIndex,
			model:     matchModel,
			date:      date,
		}

		entry, ok := aggMap[key]
		if !ok {
			entry = &dto.SpendRowRecord{
				AuthIndex: authIndex,
				Model:     model,
				Date:      date,
			}
			aggMap[key] = entry
		}
		entry.PointsSpent += p.PointsSpent
		// Only fall back to Poe-reported USD when no priced usage cost exists for the same identity/model/day.
		if !pricedUsage[key] && p.USDSpent > 0 {
			entry.USDSpent += p.USDSpent
		}
	}

	return finalizeSpendDashboard(aggMap, false), nil
}

// finalizeSpendDashboard flattens and sorts rows (newest date first).
func finalizeSpendDashboard(aggMap map[spendDashboardKey]*dto.SpendRowRecord, poeHistoryExcluded bool) *dto.SpendDashboardRecord {
	// 3. Flatten and sort rows
	rows := make([]dto.SpendRowRecord, 0, len(aggMap))
	for _, entry := range aggMap {
		rows = append(rows, *entry)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Date != rows[j].Date {
			return rows[i].Date > rows[j].Date // newest date first
		}
		if rows[i].AuthIndex != rows[j].AuthIndex {
			return rows[i].AuthIndex < rows[j].AuthIndex
		}
		return rows[i].Model < rows[j].Model
	})

	return &dto.SpendDashboardRecord{
		Rows:               rows,
		PoeHistoryExcluded: poeHistoryExcluded,
	}
}

// loadPoeIdentityPrefixesByAuthIndex 返回 Poe 身份 auth_index -> 小写 CPA 前缀（含已删除身份，以便对齐历史数据）。
func loadPoeIdentityPrefixesByAuthIndex(db *gorm.DB) (map[string]string, error) {
	type row struct {
		Identity string `gorm:"column:identity"`
		Prefix   string `gorm:"column:prefix"`
	}
	var rows []row
	if err := db.Model(&entities.UsageIdentity{}).
		Select("identity, prefix").
		Where("LOWER(TRIM(provider)) = ?", "poe").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	prefixes := make(map[string]string, len(rows))
	for _, r := range rows {
		identity := strings.TrimSpace(r.Identity)
		prefix := strings.ToLower(strings.TrimSpace(r.Prefix))
		if identity == "" || prefix == "" {
			continue
		}
		prefixes[identity] = prefix
	}
	return prefixes, nil
}

// spendModelMatchKey 把模型名归一化为合并键：小写，并去掉该身份的 Poe CPA 前缀（prefix/ 或 prefix:）。
func spendModelMatchKey(model string, poePrefix string) string {
	key := strings.ToLower(strings.TrimSpace(model))
	if poePrefix == "" {
		return key
	}
	for _, separator := range []string{"/", ":"} {
		if stripped, ok := strings.CutPrefix(key, poePrefix+separator); ok && stripped != "" {
			return stripped
		}
	}
	return key
}

// spendDailyStatProjection 只读取消费看板需要的维度与已存费用。
type spendDailyStatProjection struct {
	BucketStart          time.Time `gorm:"column:bucket_start"`
	AuthIndex            string    `gorm:"column:auth_index"`
	Model                string    `gorm:"column:model"`
	CostUSD              *float64  `gorm:"column:cost_usd"`
	UnavailableCostCount *int64    `gorm:"column:unavailable_cost_count"`
}

func loadDailyStatsForSpend(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time) ([]spendDailyStatProjection, error) {
	rows := make([]spendDailyStatProjection, 0)
	query := db.Model(&entities.UsageOverviewDailyStat{}).
		Select("bucket_start, auth_index, model, cost_usd, unavailable_cost_count").
		Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)).
		Order("bucket_start asc")
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	if authIndex := strings.TrimSpace(filter.AuthIndex); authIndex != "" {
		query = query.Where("auth_index = ?", authIndex)
	}
	if model := strings.TrimSpace(filter.Model); model != "" {
		query = query.Where("model = ?", model)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load daily stats for spend: %w", err)
	}
	return rows, nil
}
