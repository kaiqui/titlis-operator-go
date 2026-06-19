package datadog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	datadogV1 "github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/internal/queue"
)

// PubSubClient collects GCP Pub/Sub metrics from Datadog using per-request credentials.
type PubSubClient struct{}

func NewPubSubClient() *PubSubClient { return &PubSubClient{} }

// CollectAll queries all GCP Pub/Sub subscription metrics in a single batch of API calls
// and returns a snapshot per active subscription. Credentials are used only within this call.
func (c *PubSubClient) CollectAll(ctx context.Context, creds queue.DDCredentials) ([]queue.QueueSnapshot, error) {
	logger := log.FromContext(ctx)

	metricsAPI, authCtx := newDDMetricsClient(creds)

	now := time.Now().Unix()
	from := now - 600 // 10-minute window

	queries := map[string]string{
		"backlog": "avg:gcp.pubsub.subscription.num_undelivered_messages{*} by {subscription_id}",
		"age":     "avg:gcp.pubsub.subscription.oldest_unacked_message_age{*} by {subscription_id}",
		"pull":    "avg:gcp.pubsub.subscription.pull_message_operation_count{*}.as_rate() by {subscription_id}",
		"ack":     "avg:gcp.pubsub.subscription.ack_message_operation_count{*}.as_rate() by {subscription_id}",
		"dlq":     "sum:gcp.pubsub.subscription.num_undelivered_messages{subscription_id:*dlq*} by {subscription_id}",
	}

	type metricValues map[string]float64 // subscription_id → value
	results := make(map[string]metricValues)
	tagsBySubID := make(map[string]map[string]string) // subscription_id → {tag_key: tag_value}

	for key, q := range queries {
		resp, _, err := metricsAPI.QueryMetrics(authCtx, from, now, q)
		if err != nil {
			logger.Error(err, "pubsub: metrics query failed", "query", key)
			continue
		}

		mv := make(metricValues)
		for _, series := range resp.GetSeries() {
			subID := extractTagValue(series.GetTagSet(), "subscription_id")
			if subID == "" {
				subID = extractScopeValue(series.GetScope(), "subscription_id")
			}
			if subID == "" {
				continue
			}

			// Collect additional tags for snapshot enrichment (first time we see this sub)
			if _, seen := tagsBySubID[subID]; !seen {
				tags := parseTags(series.GetTagSet())
				tagsBySubID[subID] = tags
			}

			pts := series.GetPointlist()
			if len(pts) > 0 {
				last := pts[len(pts)-1]
				if len(last) >= 2 && last[1] != nil {
					mv[subID] = *last[1]
				}
			}
		}
		results[key] = mv
	}

	now64 := time.Now()
	var snapshots []queue.QueueSnapshot

	// Union of all subscription IDs seen across all queries
	seen := make(map[string]bool)
	for _, mv := range results {
		for subID := range mv {
			seen[subID] = true
		}
	}

	for subID := range seen {
		tags := tagsBySubID[subID]
		projectID := tags["project_id"]
		topicID := tags["topic_id"]

		// Determine external_id: prefer full path if available, else use the subID as-is
		externalID := subID
		if !strings.HasPrefix(subID, "projects/") && projectID != "" {
			externalID = fmt.Sprintf("projects/%s/subscriptions/%s", projectID, subID)
		}

		displayName := subName(externalID)
		isDLQ := strings.Contains(strings.ToLower(displayName), "dlq")

		snapshots = append(snapshots, queue.QueueSnapshot{
			Provider:               "gcp_pubsub",
			ExternalID:             externalID,
			DisplayName:            displayName,
			ProjectID:              projectID,
			TopicID:                topicID,
			IsDLQ:                  isDLQ,
			NumUndeliveredMessages: int64(results["backlog"][subID]),
			OldestUnackedAgeSec:    int64(results["age"][subID]),
			PullMessageCountRate:   results["pull"][subID],
			AckMessageCountRate:    results["ack"][subID],
			DeadLetterMessageCount: int64(results["dlq"][subID]),
			Labels:                 tags,
			CollectedAt:            now64,
		})
	}

	// Mark HasDLQConfigured: a regular subscription has a DLQ configured if a
	// corresponding *-dlq subscription exists in our collected set.
	dlqSet := make(map[string]bool)
	for _, s := range snapshots {
		if s.IsDLQ {
			dlqSet[s.ProjectID+"/"+s.TopicID] = true
		}
	}
	for i := range snapshots {
		if !snapshots[i].IsDLQ {
			key := snapshots[i].ProjectID + "/" + snapshots[i].TopicID
			snapshots[i].HasDLQConfigured = dlqSet[key]
		}
	}

	logger.V(1).Info("pubsub: collected snapshots", "count", len(snapshots))
	return snapshots, nil
}

// subName extracts the short subscription name from a full external_id.
// "projects/proj/subscriptions/my-name" → "my-name"
func subName(externalID string) string {
	parts := strings.Split(externalID, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return externalID
}

func extractTagValue(tags []string, key string) string {
	prefix := key + ":"
	for _, t := range tags {
		if strings.HasPrefix(t, prefix) {
			return strings.TrimPrefix(t, prefix)
		}
	}
	return ""
}

func extractScopeValue(scope, key string) string {
	// scope looks like "subscription_id:projects/...,env:prod"
	parts := strings.Split(scope, ",")
	prefix := key + ":"
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, prefix) {
			return strings.TrimPrefix(p, prefix)
		}
	}
	return ""
}

func parseTags(tags []string) map[string]string {
	out := make(map[string]string, len(tags))
	for _, t := range tags {
		idx := strings.Index(t, ":")
		if idx > 0 {
			out[t[:idx]] = t[idx+1:]
		}
	}
	return out
}

func newDDMetricsClient(creds queue.DDCredentials) (*datadogV1.MetricsApi, context.Context) {
	site := creds.Site
	if site == "" {
		site = "datadoghq.com"
	}
	cfg := datadog.NewConfiguration()
	cfg.Host = "api." + site
	apiClient := datadog.NewAPIClient(cfg)

	authCtx := context.WithValue(context.Background(), datadog.ContextAPIKeys, map[string]datadog.APIKey{
		"apiKeyAuth": {Key: creds.APIKey},
		"appKeyAuth": {Key: creds.AppKey},
	})
	return datadogV1.NewMetricsApi(apiClient), authCtx
}
