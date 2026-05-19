package config

import "fmt"

type Settings struct {
	// Kubernetes
	KubernetesNamespace   string `envconfig:"KUBERNETES_NAMESPACE" default:"titlis-system"`
	// KubernetesClusterName is resolved at startup via cluster.ResolveClusterName (env → ConfigMap seal → Node labels).
	// Set this env var explicitly to pin the name and prevent any automatic change.
	KubernetesClusterName string `envconfig:"KUBERNETES_CLUSTER_NAME" default:"unknown"`

	// Controllers
	EnableScorecardController bool `envconfig:"ENABLE_SCORECARD_CONTROLLER" default:"true"`
	EnableSLOController       bool `envconfig:"ENABLE_SLO_CONTROLLER" default:"true"`

	// Reconcile
	ReconcileIntervalSeconds int `envconfig:"RECONCILE_INTERVAL_SECONDS" default:"300"`
	DebounceSeconds          int `envconfig:"DEBOUNCE_SECONDS" default:"30"`

	// Leader election
	EnableLeaderElection    bool   `envconfig:"ENABLE_LEADER_ELECTION" default:"true"`
	LeaderElectionNamespace string `envconfig:"LEADER_ELECTION_NAMESPACE" default:"titlis"`

	// titlis-api HTTP
	TitlisAPIEnabled        bool   `envconfig:"TITLIS_API_ENABLED" default:"false"`
	TitlisAPIHost           string `envconfig:"TITLIS_API_HOST" default:"titlis-api.titlis-system.svc.cluster.local"`
	TitlisAPIHTTPPort       int    `envconfig:"TITLIS_API_HTTP_PORT" default:"8080"`
	TitlisAPIKey            string `envconfig:"TITLIS_API_API_KEY"`
	TitlisAPITimeoutSeconds int    `envconfig:"TITLIS_API_HTTP_TIMEOUT_SECONDS" default:"10"`
	TitlisAPIConnectTimeout int    `envconfig:"TITLIS_API_CONNECT_TIMEOUT_SECONDS" default:"3"`

	// Datadog
	DatadogAPIKey string `envconfig:"DD_API_KEY"`
	DatadogAppKey string `envconfig:"DD_APP_KEY"`
	DatadogSite   string `envconfig:"DD_SITE" default:"datadoghq.com"`

	// Slack
	SlackEnabled        bool   `envconfig:"SLACK_ENABLED" default:"false"`
	SlackWebhookURL     string `envconfig:"SLACK_WEBHOOK_URL"`
	SlackBotToken       string `envconfig:"SLACK_BOT_TOKEN"`
	SlackDefaultChannel string `envconfig:"SLACK_DEFAULT_CHANNEL" default:"#titlis-notifications"`
	SlackRatePerMinute  int    `envconfig:"SLACK_RATE_LIMIT_PER_MINUTE" default:"60"`
	SlackRatePerHour    int    `envconfig:"SLACK_RATE_LIMIT_PER_HOUR" default:"360"`
	SlackTimeoutSeconds int    `envconfig:"SLACK_TIMEOUT_SECONDS" default:"10"`
	SlackMaxRetries     int    `envconfig:"SLACK_MAX_RETRIES" default:"3"`
	SlackMaxMsgLength   int    `envconfig:"SLACK_MAX_MESSAGE_LENGTH" default:"3000"`

	// Auto-SLO
	EnableAutoSLOCreation    bool    `envconfig:"ENABLE_AUTO_SLO_CREATION" default:"false"`
	AutoSLODefaultTarget     float64 `envconfig:"AUTO_SLO_DEFAULT_TARGET" default:"99.0"`
	AutoSLODefaultWarning    float64 `envconfig:"AUTO_SLO_DEFAULT_WARNING" default:"99.5"`
	AutoSLODefaultTimeframe  string  `envconfig:"AUTO_SLO_DEFAULT_TIMEFRAME" default:"30d"`
	AutoSLORequireDatadogSvc bool    `envconfig:"AUTO_SLO_REQUIRE_DATADOG_SERVICE" default:"true"`
	SLOPendingPollSeconds    int     `envconfig:"AUTO_SLO_PENDING_CHANGES_POLL_INTERVAL_SECONDS" default:"30"`

	// Synthetic monitor
	SyntheticEnabled     bool    `envconfig:"ENABLE_SYNTHETIC_MONITOR" default:"false"`
	SyntheticConfigPath  string  `envconfig:"SYNTHETIC_CHECKS_CONFIG_PATH" default:"config/synthetic-checks.yaml"`
	SyntheticMonitorName string  `envconfig:"SYNTHETIC_MONITOR_NAME" default:"jeitto-homepage"`
	SyntheticMonitorURL  string  `envconfig:"SYNTHETIC_MONITOR_URL" default:"https://jeitto.com.br"`
	SyntheticIntervalSec int     `envconfig:"SYNTHETIC_MONITOR_INTERVAL_SECONDS" default:"60"`
	SyntheticTimeoutSec  float64 `envconfig:"SYNTHETIC_MONITOR_TIMEOUT_SECONDS" default:"10.0"`

	// Scorecard config
	ScorecardConfigPath string `envconfig:"SCORECARD_CONFIG_PATH" default:"config/scorecard-config.yaml"`

	// CastAI monitor
	CastAIEnabled          bool   `envconfig:"ENABLE_CASTAI_MONITOR" default:"false"`
	CastAIMonitorNamespace string `envconfig:"CASTAI_MONITOR_NAMESPACE" default:"castai-agent"`
	CastAIMonitorInterval  int    `envconfig:"CASTAI_MONITOR_INTERVAL_SECONDS" default:"60"`
	CastAIClusterName      string `envconfig:"CASTAI_CLUSTER_NAME" default:"unknown"`
	CastAIAPIKey           string `envconfig:"CASTAI_API_KEY"`
	CastAIClusterID        string `envconfig:"CASTAI_CLUSTER_ID"`

	// Log
	LogLevel  string `envconfig:"LOG_LEVEL" default:"info"`
	LogFormat string `envconfig:"LOG_FORMAT" default:"json"`
}

func (s *Settings) TitlisAPIBaseURL() string {
	return fmt.Sprintf("http://%s:%d", s.TitlisAPIHost, s.TitlisAPIHTTPPort)
}
