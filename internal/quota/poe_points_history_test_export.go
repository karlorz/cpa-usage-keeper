package quota

import (
	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
)

// ParsePoePointsHistoryPayloadForTest exports parsePoePointsHistoryPayload for package test.
func ParsePoePointsHistoryPayloadForTest(response *apicall.Response) (*PoePointsHistoryPayload, error) {
	return parsePoePointsHistoryPayload(response)
}

// PollPoePointsHistoryForIdentityForTest exports pollPoePointsHistoryForIdentity for package test.
func (s *Service) PollPoePointsHistoryForIdentityForTest(identity entities.UsageIdentity) error {
	return s.pollPoePointsHistoryForIdentity(identity)
}
