package orchestrator

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/scheduler"
)

const (
	scheduledChannelTestResultLimit  = 100
	scheduledChannelDispatchInterval = time.Second
	scheduledChannelDispatcherName   = "scheduled-channel-health-check-dispatcher"
)

type scheduledChannelCheck struct {
	channelID   int
	scheduledAt string
	occursAt    time.Time
}

type ScheduledChannelTestResult struct {
	ID          int64     `json:"id"`
	ChannelID   int       `json:"channelID"`
	ChannelName string    `json:"channelName"`
	ScheduledAt string    `json:"scheduledAt"`
	CompletedAt time.Time `json:"completedAt"`
	Latency     float64   `json:"latency"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
}

type ScheduledChannelTestServiceParams struct {
	fx.In

	Ent            *ent.Client
	ChannelService *biz.ChannelService
	Tester         *TestChannelOrchestrator
	Scheduler      *scheduler.Scheduler
}

// ScheduledChannelTestService owns the persisted daily schedules and their
// runtime dispatch. Results stay in a small in-memory ring for UI toasts;
// the normal test request and usage records remain the durable history.
type ScheduledChannelTestService struct {
	ent            *ent.Client
	channelService *biz.ChannelService
	tester         *TestChannelOrchestrator
	scheduler      *scheduler.Scheduler

	mu          sync.Mutex
	registered  map[int][]string
	lastSweepAt time.Time
	results     []ScheduledChannelTestResult
	nextID      int64
}

func NewScheduledChannelTestService(params ScheduledChannelTestServiceParams) *ScheduledChannelTestService {
	return &ScheduledChannelTestService{
		ent:            params.Ent,
		channelService: params.ChannelService,
		tester:         params.Tester,
		scheduler:      params.Scheduler,
		registered:     make(map[int][]string),
		nextID:         time.Now().UnixMilli() * 1000,
	}
}

// Start restores all channel schedules after a server restart.
func (svc *ScheduledChannelTestService) Start(ctx context.Context) error {
	ctx = authz.WithSystemBypass(ctx, "scheduled-channel-health-check-start")
	channels, err := svc.ent.Channel.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("load scheduled channel health checks: %w", err)
	}

	for _, ch := range channels {
		times, err := normalizeScheduledHealthCheckTimes(ch.Policies.ScheduledHealthChecks)
		if err != nil {
			log.Warn(ctx, "ignoring invalid scheduled channel health check",
				log.Int("channel_id", ch.ID),
				log.Cause(err),
			)
			continue
		}
		svc.replaceRuntimeSchedules(ch.ID, times)
	}

	svc.mu.Lock()
	svc.lastSweepAt = time.Now()
	svc.mu.Unlock()

	if err := svc.scheduler.Register(ctx, scheduler.TaskSpec{
		Name:        scheduledChannelDispatcherName,
		Description: "Dispatch daily channel health checks and catch up after system wake",
		FixRate:     scheduledChannelDispatchInterval,
	}, svc.dispatchScheduledTests); err != nil {
		return fmt.Errorf("register scheduled channel health check dispatcher: %w", err)
	}

	return nil
}

func (svc *ScheduledChannelTestService) GetSchedules(ctx context.Context, channelID int) ([]string, error) {
	ch, err := svc.ent.Channel.Get(ctx, channelID)
	if err != nil {
		return nil, err
	}

	return normalizeScheduledHealthCheckTimes(ch.Policies.ScheduledHealthChecks)
}

func (svc *ScheduledChannelTestService) UpdateSchedules(ctx context.Context, channelID int, times []string) ([]string, error) {
	normalized, err := normalizeScheduledHealthCheckTimes(times)
	if err != nil {
		return nil, err
	}

	if _, err := svc.channelService.UpdateChannelScheduledHealthChecks(ctx, channelID, normalized); err != nil {
		return nil, err
	}
	svc.replaceRuntimeSchedules(channelID, normalized)

	return normalized, nil
}

func (svc *ScheduledChannelTestService) ResultsAfter(after int64) ([]ScheduledChannelTestResult, int64) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	latest := after
	results := make([]ScheduledChannelTestResult, 0, len(svc.results))
	for _, result := range svc.results {
		if result.ID > latest {
			latest = result.ID
		}
		if result.ID > after {
			results = append(results, result)
		}
	}

	return results, latest
}

func normalizeScheduledHealthCheckTimes(times []string) ([]string, error) {
	normalized := make([]string, 0, len(times))
	for _, value := range times {
		value = strings.TrimSpace(value)
		parsed, err := time.Parse("15:04:05", value)
		if err != nil {
			return nil, fmt.Errorf("invalid health check time %q: expected HH:MM:SS", value)
		}
		normalized = append(normalized, parsed.Format("15:04:05"))
	}

	sort.Strings(normalized)

	return slices.Compact(normalized), nil
}

func (svc *ScheduledChannelTestService) replaceRuntimeSchedules(channelID int, times []string) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	delete(svc.registered, channelID)
	if len(times) > 0 {
		svc.registered[channelID] = slices.Clone(times)
	}
}

func (svc *ScheduledChannelTestService) dispatchScheduledTests(ctx context.Context) {
	now := time.Now()

	svc.mu.Lock()
	lastSweepAt := svc.lastSweepAt
	svc.lastSweepAt = now
	due := scheduledChannelChecksDueBetween(svc.registered, lastSweepAt, now)
	svc.mu.Unlock()

	for _, check := range due {
		svc.runScheduledTest(ctx, check.channelID, check.scheduledAt)
	}
}

func scheduledChannelChecksDueBetween(registered map[int][]string, lastSweepAt, now time.Time) []scheduledChannelCheck {
	if lastSweepAt.IsZero() || now.Before(lastSweepAt) {
		return nil
	}

	due := make([]scheduledChannelCheck, 0)
	for channelID, times := range registered {
		for _, value := range times {
			parsed, err := time.Parse("15:04:05", value)
			if err != nil {
				continue
			}

			occursAt := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, now.Location())
			if occursAt.After(now) {
				occursAt = occursAt.AddDate(0, 0, -1)
			}
			if occursAt.After(lastSweepAt) {
				due = append(due, scheduledChannelCheck{
					channelID:   channelID,
					scheduledAt: value,
					occursAt:    occursAt,
				})
			}
		}
	}

	sort.Slice(due, func(i, j int) bool {
		if !due[i].occursAt.Equal(due[j].occursAt) {
			return due[i].occursAt.Before(due[j].occursAt)
		}
		if due[i].channelID != due[j].channelID {
			return due[i].channelID < due[j].channelID
		}
		return due[i].scheduledAt < due[j].scheduledAt
	})

	return due
}

func (svc *ScheduledChannelTestService) runScheduledTest(ctx context.Context, channelID int, scheduledAt string) {
	ctx = authz.WithSystemBypass(ctx, "scheduled-channel-health-check-run")
	ch, err := svc.ent.Channel.Get(ctx, channelID)
	if err != nil {
		if !ent.IsNotFound(err) {
			log.Warn(ctx, "failed to load channel for scheduled health check", log.Int("channel_id", channelID), log.Cause(err))
		}
		return
	}
	if ch.Status == channel.StatusArchived || !slices.Contains(ch.Policies.ScheduledHealthChecks, scheduledAt) {
		return
	}

	ctx = contexts.WithSource(ctx, request.SourceTest)
	result, testErr := svc.tester.TestChannel(ctx, objects.GUID{Type: ent.TypeChannel, ID: channelID}, nil, nil, nil)
	completed := ScheduledChannelTestResult{
		ChannelID:   channelID,
		ChannelName: ch.Name,
		ScheduledAt: scheduledAt,
		CompletedAt: time.Now(),
	}
	if testErr != nil {
		completed.Error = testErr.Error()
	} else {
		completed.Latency = result.Latency
		completed.Success = result.Success
		if result.Error != nil {
			completed.Error = *result.Error
		}
	}

	svc.recordResult(completed)
	if completed.Success {
		log.Info(ctx, "scheduled channel health check succeeded",
			log.Int("channel_id", channelID),
			log.Float64("latency_seconds", completed.Latency),
		)
		return
	}
	log.Warn(ctx, "scheduled channel health check failed",
		log.Int("channel_id", channelID),
		log.String("error", completed.Error),
	)
}

func (svc *ScheduledChannelTestService) recordResult(result ScheduledChannelTestResult) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	svc.nextID++
	result.ID = svc.nextID
	svc.results = append(svc.results, result)
	if len(svc.results) > scheduledChannelTestResultLimit {
		svc.results = slices.Clone(svc.results[len(svc.results)-scheduledChannelTestResultLimit:])
	}
}
