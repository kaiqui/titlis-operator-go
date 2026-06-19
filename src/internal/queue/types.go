package queue

import "time"

// DDCredentials holds per-tenant Datadog credentials fetched from titlis-api.
// Never stored beyond the duration of a single collection cycle.
type DDCredentials struct {
	APIKey string `json:"ddApiKey"`
	AppKey string `json:"ddAppKey"`
	Site   string `json:"ddSite"`
}

// QueueConfig is the feature configuration returned by /v1/operator/queue-config.
type QueueConfig struct {
	Enabled               bool     `json:"enabled"`
	MonitorCreationEnabled bool    `json:"monitorCreationEnabled"`
	LearningCycles        int      `json:"learningCycles"`
	Providers             []string `json:"providers"`
}

// LabelRegistry maps label key → allowed values for the tenant (from /v1/operator/label-registry).
type LabelRegistry map[string][]string

// QueueIntegration is a declared block in .titlis/service.yaml (spec.integrations) that maps a
// service to the queues it owns by name pattern.
type QueueIntegration struct {
	Type   string   `json:"type" yaml:"type"`
	Match  string   `json:"match,omitempty" yaml:"match"`
	Queues []string `json:"queues" yaml:"queues"`
}

// QueueName is a known queue identifier returned by /v1/operator/queue-names, used by the
// env-var correlation scan to match against Deployment env values locally.
type QueueName struct {
	ExternalID  string `json:"externalId"`
	DisplayName string `json:"displayName"`
}

// QueueLinkHint is a fila↔workload match found in the cluster (env var / ConfigMap), posted to
// /v1/operator/queue/link-hints. Only matches are sent — never raw env.
type QueueLinkHint struct {
	ExternalID   string `json:"externalId,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	WorkloadUID  string `json:"workloadUid,omitempty"`
	WorkloadName string `json:"workloadName"`
	Namespace    string `json:"namespace,omitempty"`
}

// QueueSnapshot holds metrics collected from a single Pub/Sub subscription.
type QueueSnapshot struct {
	Provider    string
	ExternalID  string // projects/{proj}/subscriptions/{name}
	DisplayName string
	ProjectID   string
	TopicID     string
	IsDLQ       bool
	TenantID    int64

	NumUndeliveredMessages      int64
	OldestUnackedAgeSec         int64
	PullMessageCountRate        float64
	SendMessageCountRate        float64
	AckMessageCountRate         float64
	DeadLetterMessageCount      int64
	MessageRetentionDurationSec int64

	HasDLQConfigured  bool
	HasSnapshotPolicy bool
	Labels            map[string]string

	HasMonitorBacklog bool
	HasMonitorAge     bool
	HasMonitorDLQ     bool

	CollectedAt time.Time
}

// MonitorStatus records which Datadog monitors exist for a queue after EnsureMonitors.
type MonitorStatus struct {
	Backlog bool
	Age     bool
	DLQ     bool
}

// QueueThresholds are the calculated percentile-based thresholds after the MONITORING transition.
type QueueThresholds struct {
	BacklogWarning  int64     `json:"backlogWarning"`
	BacklogCritical int64     `json:"backlogCritical"`
	AgeWarningSec   int64     `json:"ageWarningSec"`
	AgeCriticalSec  int64     `json:"ageCriticalSec"`
	P50Backlog      int64     `json:"p50Backlog"`
	P75Backlog      int64     `json:"p75Backlog"`
	P95Backlog      int64     `json:"p95Backlog"`
	P50AgeSec       int64     `json:"p50AgeSec"`
	P75AgeSec       int64     `json:"p75AgeSec"`
	P95AgeSec       int64     `json:"p95AgeSec"`
	CalculatedAt    time.Time `json:"calculatedAt"`
}

// QueueLifecycle is the response from /v1/operator/queue/lifecycle.
type QueueLifecycle struct {
	State            string           `json:"state"` // DISCOVERING | LEARNING | MONITORING
	ObservationCount int              `json:"observationCount"`
	LearningTarget   int              `json:"learningTarget"`
	Thresholds       *QueueThresholds `json:"thresholds,omitempty"`
}
