package dto

// SpendRowRecord holds one aggregated spend row grouped by auth_index, model, and date.
type SpendRowRecord struct {
	AuthIndex   string  `json:"auth_index"`
	Model       string  `json:"model"`
	Date        string  `json:"date"`
	USDSpent    float64 `json:"usd_spent"`
	PointsSpent float64 `json:"points_spent"`
}

// SpendDashboardRecord holds the aggregated spend dashboard output.
type SpendDashboardRecord struct {
	Rows []SpendRowRecord `json:"rows"`
}
