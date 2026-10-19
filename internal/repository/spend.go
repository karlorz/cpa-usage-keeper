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
	windowEnd := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location()).AddDate(0, 0, 1)

	aggMap := make(map[spendDashboardKey]*dto.SpendRowRecord)

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

	// 2. Load compute points and USD from poe_points_history, grouped in SQL.
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
	if model := strings.TrimSpace(filter.Model); model != "" {
		poeQuery = poeQuery.Where("bot_name = ?", model)
	}

	if err := poeQuery.Scan(&poeRows).Error; err != nil {
		return nil, fmt.Errorf("load poe points history for spend dashboard: %w", err)
	}

	for _, p := range poeRows {
		authIndex := strings.TrimSpace(p.AuthIndex)
		model := strings.TrimSpace(p.Model)
		date := strings.TrimSpace(p.Date)
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
		entry.PointsSpent += p.PointsSpent
		// If USD was not tracked from usage events, use Poe-reported USD.
		if entry.USDSpent == 0 && p.USDSpent > 0 {
			entry.USDSpent += p.USDSpent
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

// spendDailyStatProjection 只读取消费看板需要的维度与已存费用。
type spendDailyStatProjection struct {
	BucketStart time.Time `gorm:"column:bucket_start"`
	AuthIndex   string    `gorm:"column:auth_index"`
	Model       string    `gorm:"column:model"`
	CostUSD     *float64  `gorm:"column:cost_usd"`
}

func loadDailyStatsForSpend(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time) ([]spendDailyStatProjection, error) {
	rows := make([]spendDailyStatProjection, 0)
	query := db.Model(&entities.UsageOverviewDailyStat{}).
		Select("bucket_start, auth_index, model, cost_usd").
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
