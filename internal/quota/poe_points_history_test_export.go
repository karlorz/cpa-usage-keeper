package quota

import "cpa-usage-keeper/internal/cpa/dto/apicall"

// ParsePoePointsHistoryPayloadForTest exports parsePoePointsHistoryPayload for package test.
func ParsePoePointsHistoryPayloadForTest(response *apicall.Response) (*PoePointsHistoryPayload, error) {
	return parsePoePointsHistoryPayload(response)
}
