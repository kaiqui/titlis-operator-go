package observability

import "context"

type ContainerMetrics struct {
	CPUMillicores float64
	MemoryMiB     float64
}

// MetricsProvider — hoje Datadog, amanhã Prometheus/VictoriaMetrics.
type MetricsProvider interface {
	GetContainerMetrics(ctx context.Context, name, namespace string) (ContainerMetrics, error)
	GetServiceRPM(ctx context.Context, service string, days int) (float64, error)
}
