package controller_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"

	"github.com/titlis/operator/internal/controller"
	"github.com/titlis/operator/internal/queue"
)

// --- fakes ---

type fakeQueueAPI struct {
	cfg      *queue.QueueConfig
	lifecycle *queue.QueueLifecycle // used by batch: all snaps receive this lifecycle
	creds    *queue.DDCredentials
	registry queue.LabelRegistry

	batchObserveCalls  int
	promoteCalled      bool
	evaluateCalled     bool
	evaluateThresholds queue.QueueThresholds
}

func (f *fakeQueueAPI) GetQueueConfig(_ context.Context) (*queue.QueueConfig, error) {
	return f.cfg, nil
}
func (f *fakeQueueAPI) GetDatadogConfig(_ context.Context) (*queue.DDCredentials, error) {
	return f.creds, nil
}
func (f *fakeQueueAPI) GetLabelRegistry(_ context.Context) (queue.LabelRegistry, error) {
	return f.registry, nil
}
func (f *fakeQueueAPI) RecordQueueObservationBatch(_ context.Context, snaps []queue.QueueSnapshot) (map[string]*queue.QueueLifecycle, error) {
	f.batchObserveCalls++
	result := make(map[string]*queue.QueueLifecycle, len(snaps))
	for _, s := range snaps {
		result[s.ExternalID] = f.lifecycle
	}
	return result, nil
}
func (f *fakeQueueAPI) PromoteQueueToMonitoring(_ context.Context, _, _ string) {
	f.promoteCalled = true
}
func (f *fakeQueueAPI) EvaluateQueue(_ context.Context, _ queue.QueueSnapshot, thresholds queue.QueueThresholds, _ queue.LabelRegistry) {
	f.evaluateCalled = true
	f.evaluateThresholds = thresholds
}

type fakeCollector struct {
	snapshots []queue.QueueSnapshot
}

func (f *fakeCollector) CollectAll(_ context.Context, _ queue.DDCredentials) ([]queue.QueueSnapshot, error) {
	return f.snapshots, nil
}

type fakeMonitors struct {
	ensureCalled int
}

func (f *fakeMonitors) EnsureMonitors(_ context.Context, _ queue.QueueSnapshot, _ queue.QueueThresholds, _ queue.DDCredentials, _ queue.LabelRegistry) queue.MonitorStatus {
	f.ensureCalled++
	return queue.MonitorStatus{Backlog: true, Age: true}
}

// --- helpers ---

func defaultCreds() *queue.DDCredentials {
	return &queue.DDCredentials{APIKey: "dd-key", AppKey: "dd-app", Site: "datadoghq.com"}
}

func defaultSnap() queue.QueueSnapshot {
	return queue.QueueSnapshot{
		ExternalID:  "projects/p/subscriptions/orders-sub",
		Provider:    "gcp_pubsub",
		DisplayName: "orders-sub",
		TenantID:    1,
	}
}

func newRunner(api *fakeQueueAPI, collector *fakeCollector, monitors *fakeMonitors) *controller.QueueRunner {
	return &controller.QueueRunner{
		TitlisAPI:      api,
		PubSub:         collector,
		Monitors:       monitors,
		Interval:       time.Hour, // never fires in tests
		LearningCycles: 7,
		Log:            logr.Discard(),
	}
}

// --- tests ---

// TestBatchObserve_SingleCallForAllSnapshots verifies that one cycle produces
// exactly one batch call regardless of snapshot count.
func TestBatchObserve_SingleCallForAllSnapshots(t *testing.T) {
	snaps := []queue.QueueSnapshot{defaultSnap(), defaultSnap(), defaultSnap()}
	snaps[1].ExternalID = "projects/p/subscriptions/sub-2"
	snaps[2].ExternalID = "projects/p/subscriptions/sub-3"

	api := &fakeQueueAPI{
		cfg:      &queue.QueueConfig{Enabled: true, LearningCycles: 7},
		lifecycle: &queue.QueueLifecycle{State: "DISCOVERING", ObservationCount: 1},
		creds:    defaultCreds(),
		registry: queue.LabelRegistry{},
	}
	runner := newRunner(api, &fakeCollector{snapshots: snaps}, &fakeMonitors{})

	runner.RunOnce(context.Background())

	assert.Equal(t, 1, api.batchObserveCalls, "exactly one batch call must be made for all snapshots")
}

// TestLearningPhase_PromotesAutomaticallyAfterCycles verifies that promotion to MONITORING
// happens automatically once enough cycles are collected, regardless of monitorCreationEnabled.
// Datadog monitor creation is a separate step gated by the flag.
func TestLearningPhase_PromotesAutomaticallyAfterCycles(t *testing.T) {
	api := &fakeQueueAPI{
		cfg: &queue.QueueConfig{
			Enabled:                true,
			MonitorCreationEnabled: false, // monitors disabled — but promotion must still happen
			LearningCycles:         7,
		},
		lifecycle: &queue.QueueLifecycle{
			State:            "LEARNING",
			ObservationCount: 10, // above threshold
		},
		creds:    defaultCreds(),
		registry: queue.LabelRegistry{},
	}
	monitors := &fakeMonitors{}
	runner := newRunner(api, &fakeCollector{snapshots: []queue.QueueSnapshot{defaultSnap()}}, monitors)

	runner.RunOnce(context.Background())

	assert.True(t, api.evaluateCalled, "EvaluateQueue must be called in LEARNING phase")
	assert.Equal(t, queue.QueueThresholds{}, api.evaluateThresholds, "thresholds must be empty during LEARNING")
	assert.True(t, api.promoteCalled, "PromoteQueueToMonitoring must be called once cycles are reached, regardless of monitorCreationEnabled")
	assert.Equal(t, 0, monitors.ensureCalled, "EnsureMonitors must NOT be called in LEARNING phase")
}

// TestLearningPhase_NoPromotionBeforeCyclesReached verifies that even with
// monitorCreationEnabled=true, promotion only happens once cycles are reached.
func TestLearningPhase_NoPromotionBeforeCyclesReached(t *testing.T) {
	api := &fakeQueueAPI{
		cfg: &queue.QueueConfig{
			Enabled:                true,
			MonitorCreationEnabled: true,
			LearningCycles:         7,
		},
		lifecycle: &queue.QueueLifecycle{
			State:            "LEARNING",
			ObservationCount: 3, // below threshold
		},
		creds:    defaultCreds(),
		registry: queue.LabelRegistry{},
	}
	monitors := &fakeMonitors{}
	runner := newRunner(api, &fakeCollector{snapshots: []queue.QueueSnapshot{defaultSnap()}}, monitors)

	runner.RunOnce(context.Background())

	assert.True(t, api.evaluateCalled, "EvaluateQueue must be called in LEARNING")
	assert.False(t, api.promoteCalled, "PromoteQueueToMonitoring must NOT fire before cycles reached")
}

// TestLearningPhase_PromotesWhenMonitorCreationEnabled verifies that a queue
// in LEARNING with enough cycles IS promoted when monitorCreationEnabled=true.
func TestLearningPhase_PromotesWhenMonitorCreationEnabled(t *testing.T) {
	api := &fakeQueueAPI{
		cfg: &queue.QueueConfig{
			Enabled:                true,
			MonitorCreationEnabled: true,
			LearningCycles:         7,
		},
		lifecycle: &queue.QueueLifecycle{
			State:            "LEARNING",
			ObservationCount: 7,
		},
		creds:    defaultCreds(),
		registry: queue.LabelRegistry{},
	}
	monitors := &fakeMonitors{}
	runner := newRunner(api, &fakeCollector{snapshots: []queue.QueueSnapshot{defaultSnap()}}, monitors)

	runner.RunOnce(context.Background())

	assert.True(t, api.evaluateCalled, "EvaluateQueue must be called in LEARNING")
	assert.True(t, api.promoteCalled, "PromoteQueueToMonitoring must fire when cycles reached and monitorCreationEnabled=true")
	assert.Equal(t, 0, monitors.ensureCalled, "EnsureMonitors must NOT be called in LEARNING phase")
}

// TestMonitoringPhase_SkipsMonitorsWhenCreationDisabled verifies that a queue
// in MONITORING still gets scored but EnsureMonitors is NOT called when
// monitorCreationEnabled=false.
func TestMonitoringPhase_SkipsMonitorsWhenCreationDisabled(t *testing.T) {
	thresholds := &queue.QueueThresholds{
		BacklogWarning: 100, BacklogCritical: 150,
		AgeWarningSec: 60, AgeCriticalSec: 90,
	}
	api := &fakeQueueAPI{
		cfg: &queue.QueueConfig{
			Enabled:                true,
			MonitorCreationEnabled: false,
		},
		lifecycle: &queue.QueueLifecycle{
			State:      "MONITORING",
			Thresholds: thresholds,
		},
		creds:    defaultCreds(),
		registry: queue.LabelRegistry{},
	}
	monitors := &fakeMonitors{}
	runner := newRunner(api, &fakeCollector{snapshots: []queue.QueueSnapshot{defaultSnap()}}, monitors)

	runner.RunOnce(context.Background())

	assert.True(t, api.evaluateCalled, "EvaluateQueue must be called in MONITORING regardless of monitor flag")
	assert.Equal(t, *thresholds, api.evaluateThresholds, "thresholds must be passed to EvaluateQueue")
	assert.Equal(t, 0, monitors.ensureCalled, "EnsureMonitors must NOT be called when monitorCreationEnabled=false")
}

// TestMonitoringPhase_EnsuresMonitorsWhenEnabled verifies that in MONITORING
// with monitorCreationEnabled=true, both EnsureMonitors and EvaluateQueue are called.
func TestMonitoringPhase_EnsuresMonitorsWhenEnabled(t *testing.T) {
	thresholds := &queue.QueueThresholds{
		BacklogWarning: 100, BacklogCritical: 150,
		AgeWarningSec: 60, AgeCriticalSec: 90,
	}
	api := &fakeQueueAPI{
		cfg: &queue.QueueConfig{
			Enabled:                true,
			MonitorCreationEnabled: true,
		},
		lifecycle: &queue.QueueLifecycle{
			State:      "MONITORING",
			Thresholds: thresholds,
		},
		creds:    defaultCreds(),
		registry: queue.LabelRegistry{},
	}
	monitors := &fakeMonitors{}
	runner := newRunner(api, &fakeCollector{snapshots: []queue.QueueSnapshot{defaultSnap()}}, monitors)

	runner.RunOnce(context.Background())

	assert.True(t, api.evaluateCalled, "EvaluateQueue must be called in MONITORING")
	assert.Equal(t, 1, monitors.ensureCalled, "EnsureMonitors must be called exactly once when monitorCreationEnabled=true")
}
