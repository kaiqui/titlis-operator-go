package discovery

import (
	"context"
	"time"

	"github.com/go-logr/logr"
)

// DiscoveryRunner is a manager.Runnable (leader-elected by default) that sweeps every provider on
// an interval and ships the merged graph to titlis-api. It never blocks any Reconcile — it runs on
// its own goroutine and the send is fire-and-forget at the sink.
type DiscoveryRunner struct {
	Registry *Registry
	Sink     AssetSink
	Cluster  string
	Interval time.Duration
	Log      logr.Logger
}

func (r *DiscoveryRunner) Start(ctx context.Context) error {
	r.run(ctx)
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

// RunOnce executes a single sweep. Used in tests.
func (r *DiscoveryRunner) RunOnce(ctx context.Context) { r.run(ctx) }

func (r *DiscoveryRunner) run(ctx context.Context) {
	snap := r.Registry.DiscoverAll(ctx, r.Cluster)
	r.Log.Info("discovery sweep complete",
		"assets", len(snap.Assets), "relations", len(snap.Relations), "providers", len(snap.SyncStatus))
	r.Sink.SendAssetGraph(ctx, snap)
}
