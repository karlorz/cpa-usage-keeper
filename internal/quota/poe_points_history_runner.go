package quota

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var poePointsHistoryParsedURL = mustParsePoePointsHistoryURL(poePointsHistoryBaseURL)

func mustParsePoePointsHistoryURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		return &url.URL{Scheme: "https", Host: "api.poe.com", Path: "/usage/points_history"}
	}
	return parsed
}

const (
	// poePointsHistoryDefaultPollInterval is the default polling interval between points_history scans.
	poePointsHistoryDefaultPollInterval = 60 * time.Second
	// poePointsHistoryDatabaseTimeout limits database queries during polling and upserts.
	poePointsHistoryDatabaseTimeout = 5 * time.Second
	// poePointsHistoryAPITimeout limits single HTTP request calls to Poe API.
	poePointsHistoryAPITimeout = 20 * time.Second
	// poePointsHistoryDefaultPageLimit is the maximum items requested per page.
	poePointsHistoryDefaultPageLimit = 100
	// poePointsHistoryBaseURL is the default Poe usage API points_history endpoint.
	poePointsHistoryBaseURL = "https://api.poe.com/usage/points_history"
)

// poePointsHistoryWriter abstracts writing/upserting Poe points history rows to the database.
type poePointsHistoryWriter func(context.Context, *gorm.DB, []entities.PoePointsHistory) error

// poePointsHistoryIdentityLister abstracts discovering active Poe identities to poll.
type poePointsHistoryIdentityLister func(context.Context, *gorm.DB) ([]entities.UsageIdentity, error)

// newPoePointsHistoryTimer creates a one-shot timer with stop callback.
func newPoePointsHistoryTimer(delay time.Duration) (<-chan time.Time, func()) {
	timer := time.NewTimer(delay)
	return timer.C, func() { timer.Stop() }
}

// UpsertPoePointsHistory idempotently upserts Poe points history records keyed by query_id.
// On insert: FirstSeenAt = ObservedAt = now.
// On conflict (same query_id): updates LastSeenAt only.
func UpsertPoePointsHistory(ctx context.Context, db *gorm.DB, records []entities.PoePointsHistory) error {
	if db == nil || len(records) == 0 {
		return nil
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "query_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"last_seen_at": gorm.Expr("EXCLUDED.last_seen_at"),
			}),
		}).CreateInBatches(records, 100).Error
	})
}

// listActivePoeIdentities finds all active (non-deleted, non-disabled) Poe identities.
func listActivePoeIdentities(ctx context.Context, db *gorm.DB) ([]entities.UsageIdentity, error) {
	if db == nil {
		return nil, nil
	}
	var identities []entities.UsageIdentity
	err := db.WithContext(ctx).
		Select("id, name, alias, identity, provider, type, file_name, auth_type, is_deleted, disabled").
		Where("is_deleted = ? AND (disabled IS NULL OR disabled = ?) AND provider = ?",
			false, false, "poe").
		Order("priority IS NULL ASC").
		Order("priority DESC").
		Order("id ASC").
		Find(&identities).Error
	return identities, err
}

// runPoePointsHistoryRunner periodically polls points_history for all active Poe identities.
func (s *Service) runPoePointsHistoryRunner() {
	defer close(s.poePointsHistoryDoneCh)

	for {
		timerC, stopTimer := s.poePointsHistoryNewTimer(s.poePointsHistoryPollInterval)
		select {
		case <-timerC:
			stopTimer()
			s.pollAllActivePoePointsHistory()
		case <-s.poePointsHistoryWake:
			stopTimer()
			s.pollAllActivePoePointsHistory()
		case <-s.poePointsHistoryStopCh:
			stopTimer()
			return
		}
	}
}

// pollAllActivePoePointsHistory discovers active Poe identities and polls each one.
func (s *Service) pollAllActivePoePointsHistory() {
	if s == nil || s.db == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), poePointsHistoryDatabaseTimeout)
	identities, err := s.poePointsHistoryListIdentities(ctx, s.db)
	cancel()
	if err != nil {
		logrus.WithError(err).Warn("poe points history identity listing failed")
		return
	}

	for _, identity := range identities {
		s.poePointsHistoryMu.Lock()
		closing := s.poePointsHistoryClosing
		s.poePointsHistoryMu.Unlock()
		if closing {
			return
		}

		if err := s.pollPoePointsHistoryForIdentity(identity); err != nil {
			logrus.WithError(err).WithField("auth_index", identity.Identity).Warn("poe points history poll failed for identity")
		}
	}
}

// pollPoePointsHistoryForIdentity polls pages for a single Poe identity until has_more is false or max pages reached.
func (s *Service) pollPoePointsHistoryForIdentity(identity entities.UsageIdentity) error {
	authIndex := strings.TrimSpace(identity.Identity)
	if authIndex == "" {
		return nil
	}

	startingAfter := ""
	const maxPagesPerPoll = 10
	pagesFetched := 0

	for {
		s.poePointsHistoryMu.Lock()
		closing := s.poePointsHistoryClosing
		s.poePointsHistoryMu.Unlock()
		if closing {
			return nil
		}

		endpointURL := s.buildPoePointsHistoryURL(poePointsHistoryDefaultPageLimit, startingAfter)
		ctx, cancel := context.WithTimeout(context.Background(), poePointsHistoryAPITimeout)
		response, err := s.caller.CallManagementAPI(ctx, apicall.Request{
			AuthIndex: authIndex,
			Method:    "GET",
			URL:       endpointURL,
			Header: map[string]string{
				"Authorization": "Bearer " + authIndex,
				"Accept":        "application/json",
			},
		})
		cancel()
		if err != nil {
			return fmt.Errorf("calling poe points_history API: %w", err)
		}

		payload, err := parsePoePointsHistoryPayload(response)
		if err != nil {
			return fmt.Errorf("parsing poe points_history payload: %w", err)
		}

		now := timeutil.NormalizeStorageTime(time.Now())
		queryIDs := make([]string, 0, len(payload.Data))
		records := make([]entities.PoePointsHistory, 0, len(payload.Data))
		for _, item := range payload.Data {
			if strings.TrimSpace(item.QueryID) == "" {
				continue
			}
			queryIDs = append(queryIDs, item.QueryID)
			observedAt := now
			if item.CreationTime != nil && *item.CreationTime > 0 {
				// Poe returns creation_time in microseconds or seconds; if > 1e11 treat as micros
				if *item.CreationTime > 1e11 {
					observedAt = timeutil.NormalizeStorageTime(poeUnixMicrosToTime(*item.CreationTime))
				} else {
					observedAt = timeutil.NormalizeStorageTime(time.Unix(*item.CreationTime, 0))
				}
			}

			records = append(records, entities.PoePointsHistory{
				QueryID:               item.QueryID,
				AuthIndex:             authIndex,
				BotName:               item.BotName,
				CostPoints:            item.CostPoints,
				CostUSD:               item.CostUSD,
				CostBreakdownInPoints: item.CostBreakdownInPoints,
				ObservedAt:            observedAt,
				FirstSeenAt:           now,
				LastSeenAt:            now,
			})
		}

		existingIDs := map[string]struct{}{}
		if len(queryIDs) > 0 && s.db != nil {
			lookupCtx, lookupCancel := context.WithTimeout(context.Background(), poePointsHistoryDatabaseTimeout)
			existingIDs, err = existingPoePointsHistoryQueryIDs(lookupCtx, s.db, queryIDs)
			lookupCancel()
			if err != nil {
				return fmt.Errorf("looking up existing poe points history: %w", err)
			}
		}

		if len(records) > 0 {
			writeCtx, writeCancel := context.WithTimeout(context.Background(), poePointsHistoryDatabaseTimeout)
			err := s.poePointsHistoryWrite(writeCtx, s.db, records)
			writeCancel()
			if err != nil {
				return fmt.Errorf("upserting poe points history: %w", err)
			}
		}

		pagesFetched++
		if len(existingIDs) > 0 || !payload.HasMore || pagesFetched >= maxPagesPerPoll {
			break
		}

		// Advance pagination cursor
		nextCursor := payload.NextStarting
		if nextCursor == "" {
			nextCursor = payload.StartingAfter
		}
		if nextCursor == "" && len(payload.Data) > 0 {
			nextCursor = payload.Data[len(payload.Data)-1].QueryID
		}
		if nextCursor == "" || nextCursor == startingAfter {
			break
		}
		startingAfter = nextCursor
	}

	return nil
}

func existingPoePointsHistoryQueryIDs(ctx context.Context, db *gorm.DB, queryIDs []string) (map[string]struct{}, error) {
	found := make(map[string]struct{}, len(queryIDs))
	if db == nil || len(queryIDs) == 0 {
		return found, nil
	}
	var existing []string
	if err := db.WithContext(ctx).Model(&entities.PoePointsHistory{}).
		Where("query_id IN ?", queryIDs).
		Pluck("query_id", &existing).Error; err != nil {
		return nil, err
	}
	for _, id := range existing {
		found[id] = struct{}{}
	}
	return found, nil
}

func (s *Service) buildPoePointsHistoryURL(limit int, startingAfter string) string {
	parsed := *poePointsHistoryParsedURL
	query := parsed.Query()
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if strings.TrimSpace(startingAfter) != "" {
		query.Set("starting_after", strings.TrimSpace(startingAfter))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// stopPoePointsHistoryRunner cleanly shuts down the Poe points history poller.
func (s *Service) stopPoePointsHistoryRunner() {
	if s == nil {
		return
	}
	s.poePointsHistoryCloseOnce.Do(func() {
		s.poePointsHistoryMu.Lock()
		s.poePointsHistoryClosing = true
		close(s.poePointsHistoryStopCh)
		s.poePointsHistoryMu.Unlock()
	})
	<-s.poePointsHistoryDoneCh
}

// TriggerPoePointsHistoryPoll wakes the poller to run an immediate poll cycle.
func (s *Service) TriggerPoePointsHistoryPoll() {
	if s == nil {
		return
	}
	s.poePointsHistoryMu.Lock()
	defer s.poePointsHistoryMu.Unlock()
	if s.poePointsHistoryClosing {
		return
	}
	select {
	case s.poePointsHistoryWake <- struct{}{}:
	default:
	}
}
