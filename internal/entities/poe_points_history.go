package entities

import "time"

// PoePointsHistory stores Poe compute-point spend per query, polled from Poe's usage API.
// query_id is Poe's identifier (NOT CPA's request_id) — join by identity + model + time.
type PoePointsHistory struct {
	QueryID               string    `gorm:"primaryKey;column:query_id"`
	AuthIndex             string    `gorm:"column:auth_index;index:idx_poe_points_history_auth_index_observed_at,priority:1"`
	BotName               string    `gorm:"column:bot_name;index"`
	CostPoints            *float64  `gorm:"column:cost_points"`
	CostUSD               *float64  `gorm:"column:cost_usd"`
	CostBreakdownInPoints *float64  `gorm:"column:cost_breakdown_in_points"`
	ObservedAt            time.Time `gorm:"serializer:storageTime;column:observed_at;index:idx_poe_points_history_observed_at;index:idx_poe_points_history_auth_index_observed_at,priority:2"`
	FirstSeenAt           time.Time `gorm:"serializer:storageTime;column:first_seen_at"`
	LastSeenAt            time.Time `gorm:"serializer:storageTime;column:last_seen_at"`
}

func (PoePointsHistory) TableName() string {
	return "poe_points_history"
}
