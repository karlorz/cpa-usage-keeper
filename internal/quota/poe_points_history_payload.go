package quota

import (
	"encoding/json"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

// PoePointsHistoryItem represents one query entry returned by Poe's GET /usage/points_history endpoint.
type PoePointsHistoryItem struct {
	QueryID               string   `json:"query_id"`
	BotName               string   `json:"bot_name"`
	CostPoints            *float64 `json:"cost_points"`
	CostUSD               *float64 `json:"cost_usd"`
	CostBreakdownInPoints *float64 `json:"cost_breakdown_in_points"`
	CreationTime          *int64   `json:"creation_time"`
}

// PoePointsHistoryPayload represents the top-level response of Poe's GET /usage/points_history endpoint.
type PoePointsHistoryPayload struct {
	Data         []PoePointsHistoryItem `json:"data"`
	HasMore      bool                   `json:"has_more"`
	NextStarting string                 `json:"next_starting"`
	StartingAfter string                `json:"starting_after"`
}

func parsePoePointsHistoryItem(object map[string]json.RawMessage) (PoePointsHistoryItem, bool) {
	if object == nil {
		return PoePointsHistoryItem{}, false
	}
	queryID := stringField(object, "query_id", "queryId", "id")
	if strings.TrimSpace(queryID) == "" {
		return PoePointsHistoryItem{}, false
	}

	return PoePointsHistoryItem{
		QueryID:               strings.TrimSpace(queryID),
		BotName:               stringField(object, "bot_name", "botName", "model", "bot"),
		CostPoints:            xaiFloatPtrField(object, "cost_points", "costPoints", "points_cost", "pointsCost"),
		CostUSD:               xaiFloatPtrField(object, "cost_usd", "costUsd", "usd_cost", "usdCost"),
		CostBreakdownInPoints: xaiFloatPtrField(object, "cost_breakdown_in_points", "costBreakdownInPoints", "points_breakdown", "pointsBreakdown"),
		CreationTime:          intPtrField(object, "creation_time", "creationTime", "created_at", "createdAt", "timestamp"),
	}, true
}

func parsePoePointsHistoryPayload(response *apicall.Response) (*PoePointsHistoryPayload, error) {
	object, err := parseResponseObject(response)
	if err != nil {
		return nil, err
	}
	// Poe may nest under "body" or "data"
	if nested := objectField(object, "body"); nested != nil {
		object = nested
	}

	payload := &PoePointsHistoryPayload{
		HasMore:       boolField(object, "has_more", "hasMore"),
		NextStarting:  stringField(object, "next_starting", "nextStarting", "next_cursor", "nextCursor"),
		StartingAfter: stringField(object, "starting_after", "startingAfter"),
	}

	// Try parsing entries from "data", "history", "items", "entries", or top-level array if array was wrapped
	rawItems := arrayField(object, "data", "history", "items", "entries", "points_history", "pointsHistory")
	payload.Data = make([]PoePointsHistoryItem, 0, len(rawItems))

	for _, raw := range rawItems {
		itemObject := rawObject(raw)
		if itemObject == nil {
			continue
		}
		item, ok := parsePoePointsHistoryItem(itemObject)
		if !ok {
			continue
		}
		payload.Data = append(payload.Data, item)
	}

	return payload, nil
}
