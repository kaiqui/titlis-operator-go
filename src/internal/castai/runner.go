package castai

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/observability/datadog"
)

// Runner implements manager.Runnable. It periodically checks CAST AI pod
// health and ships a castai.pod.health gauge to Datadog.
type Runner struct {
	checker *HealthChecker
	metrics *datadog.MetricsClient
	cfg     *config.Settings
}

func NewRunner(k8s client.Client, metrics *datadog.MetricsClient, cfg *config.Settings) *Runner {
	checker := NewHealthChecker(k8s, cfg.CastAIMonitorNamespace, cfg.CastAIClusterName)
	return &Runner{checker: checker, metrics: metrics, cfg: cfg}
}

// Start satisfies manager.Runnable.
func (r *Runner) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("castai-monitor")

	// Initial delay mirrors the Python operator's asyncio.sleep(10).
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(10 * time.Second):
	}

	ticker := time.NewTicker(time.Duration(r.cfg.CastAIMonitorInterval) * time.Second)
	defer ticker.Stop()

	for {
		r.runOnce(ctx, logger)
		select {
		case <-ctx.Done():
			logger.Info("CAST AI Monitor encerrado")
			return nil
		case <-ticker.C:
		}
	}
}

func (r *Runner) runOnce(ctx context.Context, logger interface{ Info(string, ...any) }) {
	results := r.checker.CheckAll(ctx)

	for _, res := range results {
		value := 0.0
		if res.IsHealthy {
			value = 1.0
		}

		tags := []string{
			"cluster_name:" + res.ClusterName,
			"service:" + res.Service,
			"namespace:" + res.Namespace,
		}

		if err := r.metrics.SendGauge("castai.pod.health", value, tags); err != nil {
			logger.Info("falha ao enviar métrica CAST AI",
				"service", res.Service, "error", err)
		} else {
			logger.Info("métrica CAST AI enviada",
				"service", res.Service,
				"healthy", res.IsHealthy,
				"reason", res.Reason,
				"pod", res.PodName,
			)
		}
	}
}
