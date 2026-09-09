package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
)

type recordingScheduledTester struct {
	calls int
	err   error
}

func (tester *recordingScheduledTester) TestChannel(ctx context.Context, _ objects.GUID, modelID *string, _ *httpclient.ProxyConfig, _ *string) (*TestChannelResult, error) {
	tester.calls++
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > scheduledChannelTestTimeout {
		return nil, errors.New("scheduled probe must have a bounded deadline")
	}
	if modelID == nil || *modelID != "gpt-5.6-luna" {
		return nil, errors.New("scheduled probe must use the recorded default model")
	}
	if tester.err != nil {
		return nil, tester.err
	}
	return &TestChannelResult{Success: true, Latency: 0.02}, nil
}

type scheduledHealthFixture struct {
	client    *ent.Client
	ctx       context.Context
	channelID int
	now       time.Time
	tester    *recordingScheduledTester
}

func newScheduledHealthFixture(testingT *testing.T) *scheduledHealthFixture {
	testingT.Helper()
	client := enttest.NewEntClient(testingT, "sqlite3", "file:scheduled-health-"+testingT.Name()+"?mode=memory&_fk=1")
	testingT.Cleanup(func() { _ = client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	created, err := client.Channel.Create().
		SetName("key").SetType(channel.TypeCodex).
		SetBaseURL("https://example.invalid").
		SetCredentials(objects.ChannelCredentials{APIKeys: []string{"test-only"}}).
		SetSupportedModels([]string{"gpt-5.6-luna"}).
		SetDefaultTestModel("gpt-5.6-luna").
		SetPolicies(objects.ChannelPolicies{ScheduledHealthChecks: []string{"09:00:00", "14:01:00"}}).
		Save(ctx)
	require.NoError(testingT, err)
	return &scheduledHealthFixture{
		client: client, ctx: ctx, channelID: created.ID,
		now:    time.Date(2026, time.September, 10, 8, 59, 0, 0, time.FixedZone("test", 8*60*60)),
		tester: &recordingScheduledTester{},
	}
}

func (fixture *scheduledHealthFixture) service() *ScheduledChannelTestService {
	svc := NewScheduledChannelTestService(ScheduledChannelTestServiceParams{Ent: fixture.client})
	svc.tester = fixture.tester
	svc.now = func() time.Time { return fixture.now }
	svc.startedAt = fixture.now
	svc.replaceRuntimeSchedules(fixture.channelID, []string{"09:00:00", "14:01:00"})
	return svc
}

func TestScheduledHealthRestoresMissedRunWithoutRepeatingAfterRestart(testingT *testing.T) {
	fixture := newScheduledHealthFixture(testingT)
	original := fixture.service()
	require.NoError(testingT, original.restoreRuntimeState(fixture.ctx))
	require.NoError(testingT, original.persistRuntimeState(fixture.ctx))
	fixture.now = fixture.now.Add(3 * time.Minute)
	restored := fixture.service()
	require.NoError(testingT, restored.restoreRuntimeState(fixture.ctx))
	restored.dispatchScheduledTests(fixture.ctx)
	require.Equal(testingT, 1, fixture.tester.calls)
	status := restored.RuntimeStatus(fixture.channelID)
	require.Equal(testingT, "succeeded", status.LastRun.Status)
	require.Equal(testingT, "09:00:00", status.LastRun.ScheduledAt)
	require.Equal(testingT, fixture.now, status.LastRun.StartedAt)
	require.Equal(testingT, 9, status.LastRun.ScheduledFor.Hour())
	require.Equal(testingT, 14, status.NextRunAt.Hour())
	require.Equal(testingT, 1, status.NextRunAt.Minute())

	fixture.now = fixture.now.Add(time.Minute)
	restarted := fixture.service()
	require.NoError(testingT, restarted.restoreRuntimeState(fixture.ctx))
	restarted.dispatchScheduledTests(fixture.ctx)
	require.Equal(testingT, 1, fixture.tester.calls)
	results, _ := restarted.ResultsAfter(0)
	require.Len(testingT, results, 1)
	require.True(testingT, results[0].Success)
}

func TestScheduledHealthCatchUpCoalescesOlderSlots(testingT *testing.T) {
	fixture := newScheduledHealthFixture(testingT)
	svc := fixture.service()
	svc.lastSweepAt = fixture.now
	fixture.now = fixture.now.Add(5*time.Hour + 3*time.Minute)
	svc.dispatchScheduledTests(fixture.ctx)
	require.Equal(testingT, 1, fixture.tester.calls)
	require.Equal(testingT, "14:01:00", svc.RuntimeStatus(fixture.channelID).LastRun.ScheduledAt)
}

func TestScheduledHealthDoesNotReplayAnUnconfirmedAttempt(testingT *testing.T) {
	fixture := newScheduledHealthFixture(testingT)
	svc := fixture.service()
	svc.lastSweepAt = fixture.now
	fixture.now = fixture.now.Add(time.Minute)
	claimed, err := svc.claimScheduledTest(fixture.ctx, ScheduledChannelTestResult{
		ChannelID: fixture.channelID, ScheduledAt: "09:00:00", ScheduledFor: fixture.now,
		StartedAt: fixture.now, Status: "running", ModelID: "gpt-5.6-luna",
	})
	require.NoError(testingT, err)
	require.True(testingT, claimed)
	fixture.now = fixture.now.Add(time.Minute)
	restarted := fixture.service()
	require.NoError(testingT, restarted.restoreRuntimeState(fixture.ctx))
	restarted.dispatchScheduledTests(fixture.ctx)
	require.Zero(testingT, fixture.tester.calls)
	lastRun := restarted.RuntimeStatus(fixture.channelID).LastRun
	require.Equal(testingT, "interrupted", lastRun.Status)
	require.Contains(testingT, lastRun.Error, "not retried automatically")
}

func TestScheduledHealthRecordsFailuresBeforeARequestExists(testingT *testing.T) {
	fixture := newScheduledHealthFixture(testingT)
	fixture.tester.err = errors.New("default test model is unavailable")
	svc := fixture.service()
	svc.lastSweepAt = fixture.now
	fixture.now = fixture.now.Add(2 * time.Minute)
	svc.dispatchScheduledTests(fixture.ctx)
	restarted := fixture.service()
	require.NoError(testingT, restarted.restoreRuntimeState(fixture.ctx))
	lastRun := restarted.RuntimeStatus(fixture.channelID).LastRun
	require.Equal(testingT, "failed", lastRun.Status)
	require.False(testingT, lastRun.Success)
	require.Equal(testingT, fixture.tester.err.Error(), lastRun.Error)
}

func TestScheduledHealthDoesNotProbeWithoutADurableClaim(testingT *testing.T) {
	fixture := newScheduledHealthFixture(testingT)
	fixture.client.System.Use(func(_ ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) {
			return nil, errors.New("checkpoint unavailable")
		})
	})
	svc := fixture.service()
	svc.lastSweepAt = fixture.now
	fixture.now = fixture.now.Add(2 * time.Minute)
	svc.dispatchScheduledTests(fixture.ctx)
	require.Zero(testingT, fixture.tester.calls)
	status := svc.RuntimeStatus(fixture.channelID)
	require.Nil(testingT, status.LastRun)
	require.Contains(testingT, status.CheckpointError, "checkpoint unavailable")
	require.Equal(testingT, 8, svc.lastSweepAt.Hour())
}

func TestScheduledHealthCheckpointsIdleProgress(testingT *testing.T) {
	fixture := newScheduledHealthFixture(testingT)
	svc := fixture.service()
	svc.lastSweepAt = fixture.now.Add(-time.Minute)
	svc.dispatchScheduledTests(fixture.ctx)
	restored := fixture.service()
	require.NoError(testingT, restored.restoreRuntimeState(fixture.ctx))
	require.True(testingT, restored.lastSweepAt.Equal(fixture.now))
	require.Zero(testingT, fixture.tester.calls)
}

func TestScheduledHealthFreshStateAndOverlappingDispatchDoNotReplay(testingT *testing.T) {
	fixture := newScheduledHealthFixture(testingT)
	fixture.now = fixture.now.Add(2 * time.Minute)
	svc := fixture.service()
	require.NoError(testingT, svc.restoreRuntimeState(fixture.ctx))
	svc.dispatchScheduledTests(fixture.ctx)
	require.Zero(testingT, fixture.tester.calls)
	svc.lastSweepAt = fixture.now.Add(-2 * time.Minute)
	svc.dispatchMu.Lock()
	svc.dispatchScheduledTests(fixture.ctx)
	svc.dispatchMu.Unlock()
	require.Zero(testingT, fixture.tester.calls)
}
