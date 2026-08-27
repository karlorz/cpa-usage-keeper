package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

type spendDashboardKey struct {
	authIndex string
	model     string
	date      string
}

// BuildSpendDashboard aggregates USD from UsageOverviewDailyStat/UsageEvent and compute points from PoePointsHistory.
func BuildSpendDashboard(ctx context.Context, db *gorm.DB, filter dto.UsageQueryFilter, costResolver pricing.Resolver) (*dto.SpendDashboardRecord, error) {
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
	windowEnd := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location()).AddDate(0, 0, 1)

	aggMap := make(map[spendDashboardKey]*dto.SpendRowRecord)

	// 1. Load USD spend from UsageOverviewDailyStats
	activeFields := costResolver.ActiveFields()
	dailyStats, err := loadDailyStatsForSpend(db.WithContext(ctx), filter, windowStart, windowEnd, activeFields)
	if err != nil {
		return nil, fmt.Errorf("load daily stats for spend dashboard: %w", err)
	}

	for _, stat := range dailyStats {
		authIndex := strings.TrimSpace(stat.AuthIndex)
		model := strings.TrimSpace(stat.Model)
		date := timeutil.NormalizeStorageTime(stat.BucketStart).Format(time.DateOnly)

		costResult := calculateAnalysisOverviewProjectionCost(costResolver, stat)
		costUSD := costResult.Cost.TotalCostUSD

		key := spendDashboardKey{
			authIndex: authIndex,
			model:     model,
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
	}

	// 2. Load compute points and USD from poe_points_history
	var poeRows []entities.PoePointsHistory
	poeQuery := db.WithContext(ctx).Model(&entities.PoePointsHistory{}).
		Where("observed_at >= ? AND observed_at < ?", timeutil.FormatStorageTime(windowStart), timeutil.FormatStorageTime(windowEnd))

	if authIndex := strings.TrimSpace(filter.AuthIndex); authIndex != "" {
		poeQuery = poeQuery.Where("auth_index = ?", authIndex)
	}
	if model := strings.TrimSpace(filter.Model); model != "" {
		poeQuery = poeQuery.Where("bot_name = ?", model)
	}

	if err := poeQuery.Find(&poeRows).Error; err != nil {
		return nil, fmt.Errorf("load poe points history for spend dashboard: %w", err)
	}

	for _, p := range poeRows {
		authIndex := strings.TrimSpace(p.AuthIndex)
		model := strings.TrimSpace(p.BotName)
		date := timeutil.NormalizeStorageTime(p.ObservedAt).Format(time.DateOnly)

		var points float64
		if p.CostPoints != nil {
			points = *p.CostPoints
		}
		var usd float64
		if p.CostUSD != nil {
			usd = *p.CostUSD
		}

		key := spendDashboardKey{
			authIndex: authIndex,
			model:     model,
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
		entry.PointsSpent += points
		// If USD was not tracked from usage events, accumulate poe history cost_usd
		if entry.USDSpent == 0 && usd > 0 {
			entry.USDSpent += usd
		}
	}

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
		Rows: rows,
	}, nil
}

func loadDailyStatsForSpend(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, activeFields pricing.ActiveFields) ([]analysisOverviewStatProjection, error) {
	query := db.Model(&entities.UsageOverviewDailyStat{})
	// Include left join with cpa_api_keys only if api_group_key filter is set or cpa_api_keys exists
	rows := make([]analysisOverviewStatProjection, 0)
	query = query.
		Select(analysisOverviewProjectionColumns(activeFields)).
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
