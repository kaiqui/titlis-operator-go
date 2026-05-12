package datadog

import (
	"context"
	"fmt"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	datadogV1 "github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"

	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/observability"
)

type MetricsClient struct {
	metricsAPI *datadogV1.MetricsApi
	authCtx    context.Context
	site       string
}

func NewMetricsClient(cfg *config.Settings) *MetricsClient {
	configuration := datadog.NewConfiguration()
	configuration.Host = "api." + cfg.DatadogSite
	apiClient := datadog.NewAPIClient(configuration)

	authCtx := context.WithValue(context.Background(), datadog.ContextAPIKeys, map[string]datadog.APIKey{
		"apiKeyAuth": {Key: cfg.DatadogAPIKey},
		"appKeyAuth": {Key: cfg.DatadogAppKey},
	})

	return &MetricsClient{
		metricsAPI: datadogV1.NewMetricsApi(apiClient),
		authCtx:    authCtx,
		site:       cfg.DatadogSite,
	}
}

// GetContainerMetrics returns average CPU (millicores) and memory (MiB) for a container
// over the last hour, filtered by container.name and kube_namespace.
func (c *MetricsClient) GetContainerMetrics(_ context.Context, name, namespace string) (observability.ContainerMetrics, error) {
	now := time.Now().Unix()
	from := now - 3600

	cpuQuery := fmt.Sprintf(
		"avg:kubernetes.cpu.usage.total{kube_namespace:%s,kube_container_name:%s}",
		namespace, name,
	)
	memQuery := fmt.Sprintf(
		"avg:kubernetes.memory.usage{kube_namespace:%s,kube_container_name:%s}",
		namespace, name,
	)

	cpuResp, _, err := c.metricsAPI.QueryMetrics(c.authCtx, from, now, cpuQuery)
	if err != nil {
		return observability.ContainerMetrics{}, fmt.Errorf("CPU query failed: %w", err)
	}

	memResp, _, err := c.metricsAPI.QueryMetrics(c.authCtx, from, now, memQuery)
	if err != nil {
		return observability.ContainerMetrics{}, fmt.Errorf("memory query failed: %w", err)
	}

	var cpuMillicores, memoryMiB float64

	if series := cpuResp.GetSeries(); len(series) > 0 {
		pts := series[0].GetPointlist()
		if n := len(pts); n > 0 {
			// CPU usage returned in nanocores → convert to millicores
			if len(pts[n-1]) >= 2 && pts[n-1][1] != nil {
				cpuMillicores = *pts[n-1][1] / 1_000_000.0
			}
		}
	}

	if series := memResp.GetSeries(); len(series) > 0 {
		pts := series[0].GetPointlist()
		if n := len(pts); n > 0 {
			// Memory returned in bytes → convert to MiB
			if len(pts[n-1]) >= 2 && pts[n-1][1] != nil {
				memoryMiB = *pts[n-1][1] / (1024 * 1024)
			}
		}
	}

	return observability.ContainerMetrics{
		CPUMillicores: cpuMillicores,
		MemoryMiB:     memoryMiB,
	}, nil
}

// GetServiceRPM returns the average requests per minute for a Datadog APM service
// over the last N days.
func (c *MetricsClient) GetServiceRPM(_ context.Context, service string, days int) (float64, error) {
	now := time.Now().Unix()
	from := now - int64(days)*86400

	query := fmt.Sprintf("sum:trace.web.request.hits{service:%s}.as_rate()", service)
	resp, _, err := c.metricsAPI.QueryMetrics(c.authCtx, from, now, query)
	if err != nil {
		return 0, fmt.Errorf("RPM query failed: %w", err)
	}

	series := resp.GetSeries()
	if len(series) == 0 {
		return 0, nil
	}
	pts := series[0].GetPointlist()
	if len(pts) == 0 {
		return 0, nil
	}

	// Average over all data points
	var sum float64
	var count int
	for _, pt := range pts {
		if len(pt) >= 2 && pt[1] != nil {
			sum += *pt[1]
			count++
		}
	}
	if count == 0 {
		return 0, nil
	}
	// Convert per-second rate to per-minute
	return (sum / float64(count)) * 60, nil
}

// SendGauge submits a gauge metric to Datadog. Implements synthetic.MetricSender.
func (c *MetricsClient) SendGauge(name string, value float64, tags []string) error {
	now := float64(time.Now().Unix())
	gaugeType := "gauge"

	payload := datadogV1.MetricsPayload{
		Series: []datadogV1.Series{
			{
				Metric: name,
				Type:   &gaugeType,
				Points: [][]*float64{{&now, &value}},
				Tags:   tags,
			},
		},
	}

	_, _, err := c.metricsAPI.SubmitMetrics(c.authCtx, payload)
	return err
}
