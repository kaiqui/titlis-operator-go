package controller

import (
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"

	"github.com/titlis/operator/internal/queue"
)

const maxQueueGoroutines = 10

// QueueAPIClient abstracts the titlisapi.Client methods used by QueueRunner.
type QueueAPIClient interface {
	GetQueueConfig(ctx context.Context) (*queue.QueueConfig, error)
	GetDatadogConfig(ctx context.Context) (*queue.DDCredentials, error)
	GetLabelRegistry(ctx context.Context) (queue.LabelRegistry, error)
	RecordQueueObservationBatch(ctx context.Context, snaps []queue.QueueSnapshot) (map[string]*queue.QueueLifecycle, error)
	PromoteQueueToMonitoring(ctx context.Context, externalID, provider string)
	EvaluateQueue(ctx context.Context, snap queue.QueueSnapshot, thresholds queue.QueueThresholds, registry queue.LabelRegistry)
}

// QueuePubSubCollector abstracts Datadog PubSub metric collection.
type QueuePubSubCollector interface {
	CollectAll(ctx context.Context, creds queue.DDCredentials) ([]queue.QueueSnapshot, error)
}

// QueueMonitorEnsurer abstracts Datadog monitor creation/update.
type QueueMonitorEnsurer interface {
	EnsureMonitors(ctx context.Context, snap queue.QueueSnapshot, thresholds queue.QueueThresholds, creds queue.DDCredentials, registry queue.LabelRegistry) queue.MonitorStatus
}

// QueueRunner is a time-based controller-runtime Runnable that discovers and evaluates
// GCP Pub/Sub queues by querying Datadog metrics. It does not interact with Kubernetes.
type QueueRunner struct {
	TitlisAPI      QueueAPIClient
	PubSub         QueuePubSubCollector
	Monitors       QueueMonitorEnsurer
	Interval       time.Duration
	LearningCycles int
	Log            logr.Logger
}

func (r *QueueRunner) Start(ctx context.Context) error {
	r.run(ctx) // run immediately on startup, then on each interval tick
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.run(ctx)
		}
	}
}

// RunOnce executes a single collection cycle. Used in tests.
func (r *QueueRunner) RunOnce(ctx context.Context) { r.run(ctx) }

func (r *QueueRunner) run(ctx context.Context) {
	logger := r.Log

	cfg, err := r.TitlisAPI.GetQueueConfig(ctx)
	if err != nil {
		logger.Error(err, "queuerunner: failed to get queue config")
		return
	}
	if cfg == nil || !cfg.Enabled {
		logger.Info("queuerunner: queue monitoring disabled — skipping cycle")
		return
	}

	learningCycles := r.LearningCycles
	if cfg.LearningCycles > 0 {
		learningCycles = cfg.LearningCycles
	}

	ddCreds, err := r.TitlisAPI.GetDatadogConfig(ctx)
	if err != nil || ddCreds == nil || ddCreds.APIKey == "" {
		logger.Info("queuerunner: Datadog config unavailable — skipping cycle")
		return
	}

	registry, err := r.TitlisAPI.GetLabelRegistry(ctx)
	if err != nil {
		logger.Error(err, "queuerunner: failed to get label registry")
		registry = queue.LabelRegistry{}
	}

	snapshots, err := r.PubSub.CollectAll(ctx, *ddCreds)
	if err != nil {
		logger.Error(err, "queuerunner: failed to collect pubsub metrics")
		return
	}
	logger.Info("queuerunner: collected snapshots", "count", len(snapshots))
	if len(snapshots) == 0 {
		return
	}

	// Single batch call: record all observations + get lifecycle state for each in one request.
	lifecycles, err := r.TitlisAPI.RecordQueueObservationBatch(ctx, snapshots)
	if err != nil {
		logger.Error(err, "queuerunner: batch observation failed")
		return
	}

	sem := make(chan struct{}, maxQueueGoroutines)
	var wg sync.WaitGroup

	for _, snap := range snapshots {
		lc, ok := lifecycles[snap.ExternalID]
		if !ok {
			continue
		}
		wg.Add(1)
		go func(s queue.QueueSnapshot, l *queue.QueueLifecycle) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r.processWithLifecycle(ctx, s, l, *ddCreds, registry, learningCycles, cfg.MonitorCreationEnabled)
		}(snap, lc)
	}
	wg.Wait()
}

func (r *QueueRunner) processWithLifecycle(
	ctx context.Context,
	snap queue.QueueSnapshot,
	lc *queue.QueueLifecycle,
	creds queue.DDCredentials,
	registry queue.LabelRegistry,
	learningCycles int,
	monitorCreationEnabled bool,
) {
	logger := r.Log.WithValues("subscription", snap.ExternalID)

	switch lc.State {
	case "DISCOVERING":
		logger.V(1).Info("queuerunner: subscription in DISCOVERING phase",
			"observation_count", lc.ObservationCount)

	case "LEARNING":
		logger.V(1).Info("queuerunner: subscription in LEARNING phase",
			"observation_count", lc.ObservationCount, "target", learningCycles)

		// Score with empty thresholds so findings are visible during learning.
		// Non-threshold rules (e.g. label compliance) produce meaningful scores immediately.
		// Threshold-based rules are skipped by scoreops when thresholds are zero.
		r.TitlisAPI.EvaluateQueue(ctx, snap, queue.QueueThresholds{}, registry)

		// Promote automatically once enough cycles are collected — this triggers threshold
		// calculation and unlocks the MONITORING state for scoring.
		// Datadog monitor creation is a separate step gated by monitorCreationEnabled.
		if lc.ObservationCount >= learningCycles {
			logger.Info("queuerunner: promoting to MONITORING",
				"observation_count", lc.ObservationCount)
			r.TitlisAPI.PromoteQueueToMonitoring(ctx, snap.ExternalID, snap.Provider)
		}

	case "MONITORING":
		if lc.Thresholds == nil {
			logger.Info("queuerunner: MONITORING state but thresholds missing — skipping evaluation")
			return
		}

		// Only create/update Datadog monitors when explicitly enabled.
		if monitorCreationEnabled {
			monStatus := r.Monitors.EnsureMonitors(ctx, snap, *lc.Thresholds, creds, registry)
			snap.HasMonitorBacklog = monStatus.Backlog
			snap.HasMonitorAge = monStatus.Age
			snap.HasMonitorDLQ = monStatus.DLQ
			logger.V(1).Info("queuerunner: monitors ensured",
				"monitor_backlog", monStatus.Backlog, "monitor_age", monStatus.Age)
		}

		r.TitlisAPI.EvaluateQueue(ctx, snap, *lc.Thresholds, registry)
		logger.V(1).Info("queuerunner: evaluation submitted")
	}
}
