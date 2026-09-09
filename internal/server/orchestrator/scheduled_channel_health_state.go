package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/system"
)

const scheduledChannelStateKey = "scheduled_channel_health_state_v1"

type scheduledChannelRuntimeState struct {
	LastSweepAt time.Time                         `json:"lastSweepAt"`
	LastRuns    map[int]ScheduledChannelTestResult `json:"lastRuns"`
	Results     []ScheduledChannelTestResult      `json:"results"`
}

type ScheduledChannelRuntimeStatus struct {
	StartedAt       time.Time                   `json:"startedAt"`
	LastDispatchAt  time.Time                   `json:"lastDispatchAt"`
	NextRunAt       *time.Time                  `json:"nextRunAt"`
	LastRun         *ScheduledChannelTestResult `json:"lastRun"`
	CheckpointError string                      `json:"checkpointError,omitempty"`
}

func (svc *ScheduledChannelTestService) RuntimeStatus(channelID int) ScheduledChannelRuntimeStatus {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	status := ScheduledChannelRuntimeStatus{
		StartedAt:       svc.startedAt,
		LastDispatchAt:  svc.lastSweepAt,
		CheckpointError: svc.checkpointError,
	}
	if lastRun, ok := svc.lastRuns[channelID]; ok {
		status.LastRun = lo.ToPtr(lastRun)
	}
	now := svc.now()
	for _, value := range svc.registered[channelID] {
		parsed, err := time.Parse("15:04:05", value)
		if err != nil {
			continue
		}
		next := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, now.Location())
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		if status.NextRunAt == nil || next.Before(*status.NextRunAt) {
			status.NextRunAt = lo.ToPtr(next)
		}
	}
	return status
}

func (svc *ScheduledChannelTestService) restoreRuntimeState(ctx context.Context) error {
	stored, err := svc.ent.System.Query().Where(system.KeyEQ(scheduledChannelStateKey)).Only(ctx)
	if ent.IsNotFound(err) {
		svc.mu.Lock()
		svc.lastSweepAt = svc.now()
		svc.mu.Unlock()
		return nil
	}
	if err != nil {
		return fmt.Errorf("load scheduled health check state: %w", err)
	}
	var state scheduledChannelRuntimeState
	if err := json.Unmarshal([]byte(stored.Value), &state); err != nil {
		return fmt.Errorf("decode scheduled health check state: %w", err)
	}
	svc.mu.Lock()
	svc.lastSweepAt = state.LastSweepAt
	if svc.lastSweepAt.IsZero() || svc.lastSweepAt.After(svc.now()) {
		svc.lastSweepAt = svc.now()
	}
	svc.results = state.Results
	if len(svc.results) > scheduledChannelTestResultLimit {
		svc.results = slices.Clone(svc.results[len(svc.results)-scheduledChannelTestResultLimit:])
	}
	for _, result := range svc.results {
		svc.nextID = max(svc.nextID, result.ID)
	}
	var interrupted []ScheduledChannelTestResult
	for channelID, result := range state.LastRuns {
		if _, exists := svc.registered[channelID]; !exists {
			continue
		}
		svc.lastRuns[channelID] = result
		if result.Status == "running" {
			result.Status = "interrupted"
			result.CompletedAt = svc.now()
			result.Error = "Server restarted before the scheduled check outcome was recorded; the same occurrence is not retried automatically"
			interrupted = append(interrupted, result)
		}
	}
	svc.mu.Unlock()
	for _, result := range interrupted {
		svc.recordResult(result)
	}
	return nil
}

func (svc *ScheduledChannelTestService) persistRuntimeState(ctx context.Context) error {
	svc.persistMu.Lock()
	defer svc.persistMu.Unlock()
	svc.mu.Lock()
	data, err := json.Marshal(scheduledChannelRuntimeState{
		LastSweepAt: svc.lastSweepAt,
		LastRuns:    svc.lastRuns,
		Results:     svc.results,
	})
	svc.mu.Unlock()
	if err == nil {
		err = svc.ent.System.Create().SetKey(scheduledChannelStateKey).SetValue(string(data)).
			OnConflict(sql.ConflictColumns(system.FieldKey)).UpdateNewValues().Exec(ctx)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if err != nil {
		svc.checkpointError = err.Error()
		return fmt.Errorf("persist scheduled health check state: %w", err)
	}
	svc.checkpointError = ""
	svc.lastPersistAt = svc.now()
	return nil
}

func (svc *ScheduledChannelTestService) claimScheduledTest(ctx context.Context, result ScheduledChannelTestResult) (bool, error) {
	svc.mu.Lock()
	previous, exists := svc.lastRuns[result.ChannelID]
	if exists && !previous.ScheduledFor.Before(result.ScheduledFor) {
		svc.mu.Unlock()
		return false, nil
	}
	svc.lastRuns[result.ChannelID] = result
	svc.mu.Unlock()
	if err := svc.persistRuntimeState(ctx); err != nil {
		svc.mu.Lock()
		if exists {
			svc.lastRuns[result.ChannelID] = previous
		} else {
			delete(svc.lastRuns, result.ChannelID)
		}
		svc.mu.Unlock()
		return false, err
	}
	return true, nil
}
