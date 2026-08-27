package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type spendRouteStub struct {
	service.UsageProvider
	dashboard  *servicedto.SpendDashboard
	lastFilter servicedto.UsageFilter
	calls      int
}

func (s *spendRouteStub) GetSpendDashboard(_ context.Context, filter servicedto.UsageFilter) (*servicedto.SpendDashboard, error) {
	s.calls++
	s.lastFilter = filter
	return s.dashboard, nil
}

func TestUsageSpendRouteAdminAccessAndQuery(t *testing.T) {
	provider := &spendRouteStub{dashboard: &servicedto.SpendDashboard{
		Rows: []servicedto.SpendRow{
			{
				AuthIndex:   "auth-test-1",
				Model:       "deepseek-v4-flash",
				Date:        "2026-08-27",
				USDSpent:    1.23,
				PointsSpent: 4567.89,
			},
		},
	}}

	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")

	// 1. Valid spend request
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/spend?range=7d&auth_index=auth-test-1&model=deepseek-v4-flash", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	if provider.calls != 1 {
		t.Fatalf("expected 1 service call, got %d", provider.calls)
	}
	if provider.lastFilter.AuthIndex != "auth-test-1" {
		t.Fatalf("expected auth_index filter auth-test-1, got %q", provider.lastFilter.AuthIndex)
	}
	if provider.lastFilter.Model != "deepseek-v4-flash" {
		t.Fatalf("expected model filter deepseek-v4-flash, got %q", provider.lastFilter.Model)
	}

	var payload struct {
		Rows []struct {
			AuthIndex   string  `json:"auth_index"`
			Model       string  `json:"model"`
			Date        string  `json:"date"`
			USDSpent    float64 `json:"usd_spent"`
			PointsSpent float64 `json:"points_spent"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(payload.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(payload.Rows))
	}
	row := payload.Rows[0]
	if row.AuthIndex != "auth-test-1" || row.Model != "deepseek-v4-flash" || row.Date != "2026-08-27" || row.USDSpent != 1.23 || row.PointsSpent != 4567.89 {
		t.Fatalf("unexpected spend row payload: %+v", row)
	}
}

func TestUsageSpendRouteHandlesMissingProviderGracefully(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/spend?range=24h", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if response.Body.String() != `{"rows":[]}` {
		t.Fatalf("expected empty rows payload, got %s", response.Body.String())
	}
}
