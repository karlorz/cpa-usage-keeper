package quota

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

// PoePointsSpendStats holds aggregated points and USD spend for a given auth_index and window.
type PoePointsSpendStats struct {
	PointsSpent float64
	USDSpent    float64
}

type poePointsSpendRow struct {
	TotalPoints float64 `gorm:"column:total_points"`
	TotalUSD    float64 `gorm:"column:total_usd"`
}

// SumPoePointsSpendByAuthIndex aggregates cost_points and cost_usd from poe_points_history.
func SumPoePointsSpendByAuthIndex(ctx context.Context, db *gorm.DB, authIndex string, start time.Time, end *time.Time) (PoePointsSpendStats, error) {
	if db == nil {
		return PoePointsSpendStats{}, fmt.Errorf("database is nil")
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return PoePointsSpendStats{}, fmt.Errorf("auth_index is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	query := db.WithContext(ctx).Model(&entities.PoePointsHistory{}).
		Select("COALESCE(SUM(cost_points), 0) AS total_points, COALESCE(SUM(cost_usd), 0) AS total_usd").
		Where("auth_index = ?", authIndex)

	if !start.IsZero() {
		query = query.Where("observed_at >= ?", timeutil.FormatStorageTime(start))
	}
	if end != nil && !end.IsZero() {
		query = query.Where("observed_at < ?", timeutil.FormatStorageTime(*end))
	}

	var row poePointsSpendRow
	if err := query.Scan(&row).Error; err != nil {
		return PoePointsSpendStats{}, err
	}
	return PoePointsSpendStats{
		PointsSpent: row.TotalPoints,
		USDSpent:    row.TotalUSD,
	}, nil
}

// hasPoeQuotaRows returns true if the quota rows represent a Poe compute-points quota response.
func hasPoeQuotaRows(rows []QuotaRow) bool {
	for _, r := range rows {
		switch r.Key {
		case "current_point_balance", "plan_points_balance", "addon_point_balance", "total_balance_usd", "points_spent_cycle", "usd_spent_cycle":
			return true
		}
	}
	return false
}

// findPoeCycleStartTime determines the start time of the current Poe billing cycle.
// If next_monthly_grant has ResetAt and window seconds, cycleStart is (ResetAt - windowSeconds).
// Otherwise, falls back to the beginning of the current calendar month in local storage time.
func findPoeCycleStartTime(rows []QuotaRow, now time.Time) time.Time {
	now = timeutil.NormalizeStorageTime(now)
	for _, r := range rows {
		if r.Key == "next_monthly_grant" && r.ResetAt != "" && r.Window != nil && r.Window.Seconds != nil && *r.Window.Seconds > 0 {
			parsedResetAt, err := timeutil.ParseStorageTime(r.ResetAt)
			if err == nil {
				resetAt := timeutil.NormalizeStorageTime(parsedResetAt)
				cycleStart := resetAt.Add(-time.Duration(*r.Window.Seconds) * time.Second)
				if !cycleStart.IsZero() {
					return cycleStart
				}
			}
		}
	}
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
}

// appendOrUpdatePoeSpendRows inserts or updates points_spent_cycle and usd_spent_cycle QuotaRows.
func appendOrUpdatePoeSpendRows(response CheckResponse, stats PoePointsSpendStats) CheckResponse {
	pointsSpent := stats.PointsSpent
	usdSpentCents := stats.USDSpent * 100

	pointsRow := QuotaRow{
		Key:    "points_spent_cycle",
		Label:  "Points Spent (Cycle)",
		Scope:  "billing",
		Metric: "points",
		Used:   &pointsSpent,
	}
	usdRow := QuotaRow{
		Key:    "usd_spent_cycle",
		Label:  "USD Spent (Cycle)",
		Scope:  "billing",
		Metric: "usd_cents",
		Used:   &usdSpentCents,
	}

	pointsIndex := -1
	usdIndex := -1
	for i, r := range response.Quota {
		if r.Key == "points_spent_cycle" {
			pointsIndex = i
		} else if r.Key == "usd_spent_cycle" {
			usdIndex = i
		}
	}

	if pointsIndex >= 0 {
		response.Quota[pointsIndex] = pointsRow
	}
	if usdIndex >= 0 {
		response.Quota[usdIndex] = usdRow
	}

	if pointsIndex < 0 && usdIndex < 0 {
		grantIndex := -1
		for i, r := range response.Quota {
			if r.Key == "next_monthly_grant" {
				grantIndex = i
				break
			}
		}
		if grantIndex >= 0 {
			newQuota := make([]QuotaRow, 0, len(response.Quota)+2)
			newQuota = append(newQuota, response.Quota[:grantIndex]...)
			newQuota = append(newQuota, pointsRow, usdRow)
			newQuota = append(newQuota, response.Quota[grantIndex:]...)
			response.Quota = newQuota
		} else {
			response.Quota = append(response.Quota, pointsRow, usdRow)
		}
	} else if pointsIndex < 0 {
		response.Quota = append(response.Quota, pointsRow)
	} else if usdIndex < 0 {
		response.Quota = append(response.Quota, usdRow)
	}

	return response
}

// attachPoeSpendStats computes cycle spend from poe_points_history and enriches Poe quota responses.
func (s *Service) attachPoeSpendStats(ctx context.Context, authIndex string, response CheckResponse, now time.Time) CheckResponse {
	if s == nil || s.db == nil || len(response.Quota) == 0 {
		return response
	}
	if !hasPoeQuotaRows(response.Quota) {
		return response
	}
	cycleStart := findPoeCycleStartTime(response.Quota, now)
	stats, err := SumPoePointsSpendByAuthIndex(ctx, s.db, authIndex, cycleStart, nil)
	if err != nil {
		return response
	}
	// Only add spend rows if spend stats are non-zero or spend rows already exist
	if stats.PointsSpent == 0 && stats.USDSpent == 0 {
		hasExisting := false
		for _, r := range response.Quota {
			if r.Key == "points_spent_cycle" || r.Key == "usd_spent_cycle" {
				hasExisting = true
				break
			}
		}
		if !hasExisting {
			return response
		}
	}
	return appendOrUpdatePoeSpendRows(response, stats)
}
