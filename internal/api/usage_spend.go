package api

import (
	"net/http"
	"time"

	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/gin-gonic/gin"
)

type spendDashboardResponse struct {
	Rows []spendDashboardRowResponse `json:"rows"`
}

type spendDashboardRowResponse struct {
	AuthIndex   string  `json:"auth_index"`
	Model       string  `json:"model"`
	Date        string  `json:"date"`
	USDSpent    float64 `json:"usd_spent"`
	PointsSpent float64 `json:"points_spent"`
}

func registerUsageSpendRoute(router gin.IRoutes, usageProvider service.UsageProvider, cpaAPIKeyProvider service.CPAAPIKeyProvider) {
	router.GET("/usage/spend", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, spendDashboardResponse{Rows: []spendDashboardRowResponse{}})
			return
		}

		filter, err := parseUsageAnalysisTimeFilterQuery(c.Request, timeutil.NormalizeStorageTime(time.Now()))
		if err != nil {
			writeUsageFilterParseError(c, err)
			return
		}

		query := c.Request.URL.Query()
		if authIndex := query.Get("auth_index"); authIndex != "" {
			filter.AuthIndex = authIndex
		}
		if model := query.Get("model"); model != "" {
			filter.Model = model
		}

		dashboard, err := usageProvider.GetSpendDashboard(c.Request.Context(), filter)
		if err != nil {
			writeInternalError(c, "get spend dashboard failed", err)
			return
		}

		c.JSON(http.StatusOK, buildSpendDashboardPayload(dashboard))
	})
}

func buildSpendDashboardPayload(dashboard *servicedto.SpendDashboard) spendDashboardResponse {
	if dashboard == nil || len(dashboard.Rows) == 0 {
		return spendDashboardResponse{Rows: []spendDashboardRowResponse{}}
	}
	rows := make([]spendDashboardRowResponse, 0, len(dashboard.Rows))
	for _, r := range dashboard.Rows {
		rows = append(rows, spendDashboardRowResponse{
			AuthIndex:   r.AuthIndex,
			Model:       r.Model,
			Date:        r.Date,
			USDSpent:    r.USDSpent,
			PointsSpent: r.PointsSpent,
		})
	}
	return spendDashboardResponse{Rows: rows}
}
