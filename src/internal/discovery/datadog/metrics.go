package datadog

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	datadogV1 "github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
)

// classifyMetric maps a Datadog metric name to a coverage category (deterministic). Kept in sync
// with the scoreops knowledge ruleset — small enough to duplicate; classification could later move
// fully to scoreops over raw names.
func classifyMetric(name string) string {
	switch {
	case strings.HasPrefix(name, "jvm."):
		return "jvm"
	case strings.HasPrefix(name, "http.") || strings.HasPrefix(name, "trace.") && strings.Contains(name, ".request"):
		return "http"
	case hasAnyPrefix(name, "kafka.", "pubsub.", "sqs.", "rabbitmq."):
		return "messaging"
	case hasAnyPrefix(name, "postgresql.", "postgres.", "mysql.", "mongodb.", "redis."):
		return "database"
	case hasAnyPrefix(name, "system.", "container.", "cpu.", "memory.", "disk.", "network."):
		return "infra"
	default:
		return ""
	}
}

// metricCapabilities turns a service's active metric names into coverage categories + the capability
// flags ("metrics" when any metric exists, "tracing" when APM trace metrics are present).
func metricCapabilities(names []string) (categories, capabilities []string) {
	catSet := map[string]bool{}
	hasTrace := false
	for _, n := range names {
		if c := classifyMetric(n); c != "" {
			catSet[c] = true
		}
		if strings.HasPrefix(n, "trace.") {
			hasTrace = true
		}
	}
	for c := range catSet {
		categories = append(categories, c)
	}
	sort.Strings(categories)
	if len(names) > 0 {
		capabilities = append(capabilities, "metrics")
	}
	if hasTrace {
		capabilities = append(capabilities, "tracing")
	}
	return categories, capabilities
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// serviceMetricCapabilities queries the service's active metrics (one call, tag-filtered) and
// derives its categories + capabilities. Best-effort: on error returns empty (→ N/A downstream).
func (p *Provider) serviceMetricCapabilities(ctx context.Context, client *datadog.APIClient, svc string) (categories, capabilities []string) {
	api := datadogV1.NewMetricsApi(client)
	from := time.Now().Add(-p.opts.MetricsWindow).Unix()
	resp, _, err := api.ListActiveMetrics(ctx, from,
		*datadogV1.NewListActiveMetricsOptionalParameters().WithTagFilter("service:"+svc))
	if err != nil {
		return nil, nil
	}
	return metricCapabilities(resp.GetMetrics())
}
