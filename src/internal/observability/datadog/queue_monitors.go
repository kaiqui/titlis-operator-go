package datadog

import (
	"context"
	"fmt"
	"strings"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	datadogV1 "github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/internal/queue"
)

// MonitorManager creates and updates Datadog monitors for queue monitoring idempotently.
type MonitorManager struct{}

func NewMonitorManager() *MonitorManager { return &MonitorManager{} }

// EnsureMonitors creates or updates the 3 standard monitors for a queue in MONITORING state.
// Returns which monitor types exist after the operation.
// Fire-and-forget on create/update errors — we still return the status.
func (m *MonitorManager) EnsureMonitors(
	ctx context.Context,
	snap queue.QueueSnapshot,
	thresholds queue.QueueThresholds,
	creds queue.DDCredentials,
	registry queue.LabelRegistry,
) queue.MonitorStatus {
	logger := log.FromContext(ctx)

	site := creds.Site
	if site == "" {
		site = "datadoghq.com"
	}
	cfg := datadog.NewConfiguration()
	cfg.Host = "api." + site
	apiClient := datadog.NewAPIClient(cfg)
	authCtx := context.WithValue(ctx, datadog.ContextAPIKeys, map[string]datadog.APIKey{
		"apiKeyAuth": {Key: creds.APIKey},
		"appKeyAuth": {Key: creds.AppKey},
	})
	monitorsAPI := datadogV1.NewMonitorsApi(apiClient)

	// Build the set of identifying tags used for lookup
	managedTags := fmt.Sprintf(
		"managed_by:titlis,titlis_feature:queue_monitoring,titlis_tenant_id:%d,subscription_id:%s",
		snap.TenantID, snap.ExternalID,
	)

	// Lookup existing monitors
	existing, _, err := monitorsAPI.ListMonitors(authCtx,
		*datadogV1.NewListMonitorsOptionalParameters().WithMonitorTags(managedTags))
	if err != nil {
		logger.Error(err, "monitors: list failed", "subscription", snap.ExternalID)
	}

	existingByName := make(map[string]int64)
	for _, mon := range existing {
		existingByName[mon.GetName()] = mon.GetId()
	}

	// Extra tags from LabelRegistry for monitor tagging
	extraTags := buildRegistryTags(snap, registry)

	baseTags := []string{
		"managed_by:titlis",
		"titlis_feature:queue_monitoring",
		fmt.Sprintf("titlis_tenant_id:%d", snap.TenantID),
		fmt.Sprintf("subscription_id:%s", snap.ExternalID),
		fmt.Sprintf("provider:%s", snap.Provider),
	}
	allTags := append(baseTags, extraTags...)

	var status queue.MonitorStatus

	// Monitor 1: Backlog
	backlogName := fmt.Sprintf("[Titlis] PubSub Backlog por Subscription — %s", subName(snap.ExternalID))
	if id, ok := existingByName[backlogName]; ok {
		status.Backlog = true
		if thresholds.BacklogWarning > 0 {
			m.updateMonitor(ctx, monitorsAPI, authCtx, id, thresholds.BacklogWarning, thresholds.BacklogCritical)
		}
	} else {
		if err := m.createMetricMonitor(ctx, monitorsAPI, authCtx,
			backlogName,
			fmt.Sprintf("avg:gcp.pubsub.subscription.num_undelivered_messages{subscription_id:%s}", snap.ExternalID),
			"subscription_id",
			thresholds.BacklogWarning,
			thresholds.BacklogCritical,
			allTags,
		); err != nil {
			logger.Error(err, "monitors: create backlog failed", "subscription", snap.ExternalID)
		} else {
			status.Backlog = true
		}
	}

	// Monitor 2: Message Age
	ageName := fmt.Sprintf("[Titlis] PubSub Mensagem Mais Antiga — %s", subName(snap.ExternalID))
	if id, ok := existingByName[ageName]; ok {
		status.Age = true
		if thresholds.AgeWarningSec > 0 {
			m.updateMonitor(ctx, monitorsAPI, authCtx, id, thresholds.AgeWarningSec, thresholds.AgeCriticalSec)
		}
	} else {
		if err := m.createMetricMonitor(ctx, monitorsAPI, authCtx,
			ageName,
			fmt.Sprintf("avg:gcp.pubsub.subscription.oldest_unacked_message_age{subscription_id:%s}", snap.ExternalID),
			"subscription_id",
			thresholds.AgeWarningSec,
			thresholds.AgeCriticalSec,
			allTags,
		); err != nil {
			logger.Error(err, "monitors: create age failed", "subscription", snap.ExternalID)
		} else {
			status.Age = true
		}
	}

	// Monitor 3: DLQ (only for DLQ subscriptions)
	if snap.IsDLQ {
		dlqName := fmt.Sprintf("[Titlis] PubSub DLQ com Itens — %s", subName(snap.ExternalID))
		if id, ok := existingByName[dlqName]; ok {
			status.DLQ = true
			_ = id
		} else {
			if err := m.createMetricMonitor(ctx, monitorsAPI, authCtx,
				dlqName,
				fmt.Sprintf("sum:gcp.pubsub.subscription.num_undelivered_messages{subscription_id:%s}", snap.ExternalID),
				"subscription_id",
				0,  // warning: any message in DLQ
				10, // critical: >= 10 messages
				allTags,
			); err != nil {
				logger.Error(err, "monitors: create dlq failed", "subscription", snap.ExternalID)
			} else {
				status.DLQ = true
			}
		}
	}

	return status
}

func (m *MonitorManager) createMetricMonitor(
	ctx context.Context,
	api *datadogV1.MonitorsApi,
	authCtx context.Context,
	name, query, groupBy string,
	warnThreshold, critThreshold int64,
	tags []string,
) error {
	monType := datadogV1.MONITORTYPE_METRIC_ALERT
	body := datadogV1.Monitor{
		Name:  &name,
		Type:  monType,
		Query: fmt.Sprintf("avg(last_5m):%s by {%s} > %d", query, groupBy, critThreshold),
		Options: &datadogV1.MonitorOptions{
			Thresholds: &datadogV1.MonitorThresholds{
				Critical: datadog.PtrFloat64(float64(critThreshold)),
				Warning:  *datadog.NewNullableFloat64(datadog.PtrFloat64(float64(warnThreshold))),
			},
			NotifyNoData:       datadog.PtrBool(false),
			RequireFullWindow:  datadog.PtrBool(false),
		},
		Tags: tags,
	}

	_, _, err := api.CreateMonitor(authCtx, body)
	if err != nil {
		return fmt.Errorf("create monitor %q: %w", name, err)
	}
	log.FromContext(ctx).Info("monitors: created", "name", name)
	return nil
}

func (m *MonitorManager) updateMonitor(
	ctx context.Context,
	api *datadogV1.MonitorsApi,
	authCtx context.Context,
	monitorID, warn, crit int64,
) {
	update := datadogV1.MonitorUpdateRequest{
		Options: &datadogV1.MonitorOptions{
			Thresholds: &datadogV1.MonitorThresholds{
				Critical: datadog.PtrFloat64(float64(crit)),
				Warning:  *datadog.NewNullableFloat64(datadog.PtrFloat64(float64(warn))),
			},
		},
	}
	if _, _, err := api.UpdateMonitor(authCtx, monitorID, update); err != nil {
		log.FromContext(ctx).Error(err, "monitors: update failed", "monitor_id", monitorID)
	}
}

func buildRegistryTags(snap queue.QueueSnapshot, registry queue.LabelRegistry) []string {
	requiredKeys := []string{"env", "team", "service"}
	var tags []string
	for _, key := range requiredKeys {
		if val, ok := snap.Labels[key]; ok && val != "" {
			if vals, ok := registry[key]; ok {
				for _, v := range vals {
					if v == val {
						tags = append(tags, key+":"+val)
						break
					}
				}
			}
		}
	}
	return tags
}

// Ensure the package-level helper is usable — subName is defined in pubsub.go.
var _ = strings.HasSuffix
