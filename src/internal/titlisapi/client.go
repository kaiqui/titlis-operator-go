package titlisapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/discovery"
	"github.com/titlis/operator/internal/model"
	"github.com/titlis/operator/internal/queue"
	"github.com/titlis/operator/internal/servicedef"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	timeout time.Duration
}

// idleConnTimeout closes keep-alive connections before the Netty server's
// requestReadTimeout fires (120 s), avoiding spurious 408 responses on reuse.
const idleConnTimeout = 20 * time.Second

func newTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.IdleConnTimeout = idleConnTimeout
	return t
}

func New(cfg *config.Settings) *Client {
	timeout := time.Duration(cfg.TitlisAPITimeoutSeconds) * time.Second
	return &Client{
		baseURL: cfg.TitlisAPIBaseURL(),
		apiKey:  cfg.TitlisAPIKey,
		http:    &http.Client{Timeout: timeout, Transport: newTransport()},
		timeout: timeout,
	}
}

// NewWithBaseURL creates a client with an explicit base URL — useful in tests.
func NewWithBaseURL(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: timeout},
		timeout: timeout,
	}
}

// --- envelope ---

type envelope struct {
	V      int    `json:"v"`
	T      string `json:"t"`
	Ts     int64  `json:"ts"`
	APIKey string `json:"api_key"`
	Data   any    `json:"data"`
}

// --- payload types ---

type ScorecardEvaluatedPayload struct {
	WorkloadID         string               `json:"workload_id"`
	Namespace          string               `json:"namespace"`
	Workload           string               `json:"workload"`
	Cluster            string               `json:"cluster"`
	Environment        string               `json:"environment"`
	K8sEventType       string               `json:"k8s_event_type"`
	OverallScore       float64              `json:"overall_score"`
	ComplianceStatus   string               `json:"compliance_status"` // UPPERCASE
	TotalRules         int                  `json:"total_rules"`
	PassedRules        int                  `json:"passed_rules"`
	FailedRules        int                  `json:"failed_rules"`
	CriticalFailures   int                  `json:"critical_failures"`
	ErrorCount         int                  `json:"error_count"`
	WarningCount       int                  `json:"warning_count"`
	ScorecardVersion   int                  `json:"scorecard_version"`
	WorkloadKind       string               `json:"workload_kind"`
	ResourceVersion    string               `json:"resource_version"`
	Labels             map[string]string    `json:"labels"`
	Annotations        map[string]string    `json:"annotations"`
	DDGitRepositoryURL *string              `json:"dd_git_repository_url,omitempty"`
	PillarScores       []PillarScorePayload `json:"pillar_scores"`
	ValidationResults  []ValidationPayload  `json:"validation_results"`
	EvaluatedAt        time.Time            `json:"evaluated_at"`
}

type PillarScorePayload struct {
	Pillar        string  `json:"pillar"`         // strings.ToUpper
	Score         float64 `json:"score"`
	PassedChecks  int     `json:"passed_checks"`
	FailedChecks  int     `json:"failed_checks"`
	WeightedScore float64 `json:"weighted_score"`
}

type ValidationPayload struct {
	RuleID              string  `json:"rule_id"`
	RuleName            string  `json:"rule_name"`
	Pillar              string  `json:"pillar"`               // UPPERCASE
	Passed              bool    `json:"passed"`
	Severity            string  `json:"severity"`             // UPPERCASE
	RuleType            string  `json:"rule_type"`            // UPPERCASE
	Weight              float64 `json:"weight"`
	Message             string  `json:"message"`
	ActualValue         *string `json:"actual_value"`
	IsRemediable        bool    `json:"is_remediable"`
	RemediationCategory *string `json:"remediation_category"`
}

type SLOReconciledPayload struct {
	SLOID    string `json:"slo_id"`
	Service  string `json:"service"`
	Target   float64 `json:"target"`
	State    string `json:"state"`
}

type RemediationEventPayload struct {
	PRNumber int    `json:"pr_number"`
	Status   string `json:"status"`
}

type NotificationLogPayload struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Channel  string `json:"channel"`
}

type ResourceMetricsPayload struct {
	CPUMillicores float64 `json:"cpu_millicores"`
	MemoryMiB     float64 `json:"memory_mib"`
	Namespace     string  `json:"namespace"`
}

type NamespaceExclusionsSyncPayload struct {
	Cluster            string   `json:"cluster"`
	ExcludedNamespaces []string `json:"excluded_namespaces"`
}

type SLOPendingChange struct {
	ID            string `json:"id"`
	SLOConfigName string `json:"slo_config_name"`
	Namespace     string `json:"namespace"`
	Field         string `json:"field"`
	OldValue      string `json:"old_value"`
	NewValue      string `json:"new_value"`
	RequestedBy   string `json:"requested_by"`
}

// --- public API ---

func (c *Client) SendScorecardEvaluated(ctx context.Context, p ScorecardEvaluatedPayload) {
	c.post(ctx, "scorecard_evaluated", p)
}

// EvaluateWorkload forwards a WorkloadSnapshot to titlis-api for async scoring by titlis-scoreops.
// The API responds 202 immediately; results are pushed back via the scorecard_evaluated event path.
func (c *Client) EvaluateWorkload(ctx context.Context, snap any) {
	logger := log.FromContext(ctx)

	body, err := json.Marshal(snap)
	if err != nil {
		logger.Error(err, "titlisapi: EvaluateWorkload marshal failed")
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		c.baseURL+"/v1/operator/scoring/evaluate", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		logger.Error(err, "titlisapi: EvaluateWorkload send failed")
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusAccepted {
		logger.Info("titlisapi: EvaluateWorkload unexpected status", "status", resp.StatusCode)
	} else {
		logger.V(1).Info("titlisapi: EvaluateWorkload sent", "status", resp.StatusCode)
	}
}

func (c *Client) SendRemediationEvent(ctx context.Context, p RemediationEventPayload) {
	c.post(ctx, "remediation_updated", p)
}

func (c *Client) SendSLOReconciled(ctx context.Context, p SLOReconciledPayload) {
	c.post(ctx, "slo_reconciled", p)
}

func (c *Client) SendNotificationLog(ctx context.Context, p NotificationLogPayload) {
	c.post(ctx, "notification_sent", p)
}

func (c *Client) SendResourceMetrics(ctx context.Context, p ResourceMetricsPayload) {
	c.post(ctx, "resource_metrics", p)
}

func (c *Client) SendNamespaceExclusionsSync(ctx context.Context, p NamespaceExclusionsSyncPayload) {
	c.post(ctx, "namespace_exclusions_sync", p)
}

// ServiceDefinitionSyncedPayload is the data sent in the service_definition_synced event.
type ServiceDefinitionSyncedPayload struct {
	ServiceName   string                           `json:"service_name"`
	Team          string                           `json:"team"`
	Product       string                           `json:"product,omitempty"`
	Tier          string                           `json:"tier,omitempty"`
	Description   string                           `json:"description,omitempty"`
	RepoURL       string                           `json:"repo_url,omitempty"`
	Workloads     []string                         `json:"workloads"`
	RawYAML       string                           `json:"raw_yaml,omitempty"`
	Integrations  []queue.QueueIntegration         `json:"integrations,omitempty"`
	WorkloadMatch *servicedef.WorkloadMatch        `json:"workload_match,omitempty"`
	GitopsPaths   map[string]servicedef.GitopsPath `json:"gitops_paths,omitempty"`
	Remediation   map[string]interface{}           `json:"remediation,omitempty"`
}

func (c *Client) SendServiceDefinitionSynced(ctx context.Context, p ServiceDefinitionSyncedPayload) {
	c.post(ctx, "service_definition_synced", p)
}

// SendAssetGraph posts the discovered asset graph to titlis-api. Fire-and-forget: failures are
// logged but never block the discovery sweep. Uses a longer timeout because large clusters produce
// large graphs (same approach as the queue observe batch path).
func (c *Client) SendAssetGraph(ctx context.Context, snap discovery.AssetGraphSnapshot) {
	logger := log.FromContext(ctx)

	body, err := json.Marshal(snap)
	if err != nil {
		logger.Error(err, "titlisapi: SendAssetGraph marshal failed")
		return
	}

	sendClient := &http.Client{Timeout: 2 * time.Minute, Transport: c.http.Transport}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/operator/discovery/assets", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := sendClient.Do(req)
	if err != nil {
		logger.Error(err, "titlisapi: SendAssetGraph send failed")
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		logger.Info("titlisapi: SendAssetGraph unexpected status", "status", resp.StatusCode)
	} else {
		logger.V(1).Info("titlisapi: SendAssetGraph sent",
			"assets", len(snap.Assets), "relations", len(snap.Relations), "status", resp.StatusCode)
	}
}

// OperatorAIConfig holds the GitHub integration config returned by /v1/operator/ai-config.
type OperatorAIConfig struct {
	GitHubToken      string `json:"githubToken"`
	GitHubBaseBranch string `json:"githubBaseBranch"`
}

func (c *Client) GetGitHubToken(ctx context.Context) (string, error) {
	url := c.baseURL + "/v1/operator/ai-config"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("get ai-config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get ai-config: status %d", resp.StatusCode)
	}

	var cfg OperatorAIConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return "", fmt.Errorf("decode ai-config: %w", err)
	}
	return cfg.GitHubToken, nil
}

func (c *Client) GetPendingSLOChanges(ctx context.Context) ([]SLOPendingChange, error) {
	url := c.baseURL + "/v1/operator/pending-slo-changes"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("pending-slo-changes: status %d", resp.StatusCode)
	}

	var changes []SLOPendingChange
	if err := json.NewDecoder(resp.Body).Decode(&changes); err != nil {
		return nil, err
	}
	return changes, nil
}

func (c *Client) ConfirmSLOChangeApplied(ctx context.Context, changeID string) error {
	return c.postConfirm(ctx, changeID, "applied", nil)
}

func (c *Client) ConfirmSLOChangeFailed(ctx context.Context, changeID, errMsg string) error {
	return c.postConfirm(ctx, changeID, "failed", map[string]string{"error": errMsg})
}

// --- internal ---

func (c *Client) post(ctx context.Context, eventType string, data any) {
	logger := log.FromContext(ctx)
	start := time.Now()

	body, err := json.Marshal(envelope{
		V:      1,
		T:      eventType,
		Ts:     time.Now().UnixMilli(),
		APIKey: c.apiKey,
		Data:   data,
	})
	if err != nil {
		logger.Error(err, "titlisapi: marshal failed", "event", eventType)
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		c.baseURL+"/v1/operator/events", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		logger.Error(err, "titlisapi: send failed", "event", eventType, "elapsed_ms", elapsed)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		logger.Info("titlisapi: bad status",
			"event", eventType, "status", resp.StatusCode, "elapsed_ms", elapsed)
		return
	}
	logger.V(1).Info("titlisapi: sent", "event", eventType, "status", resp.StatusCode, "elapsed_ms", elapsed)
}

func (c *Client) postConfirm(ctx context.Context, changeID, action string, body any) error {
	url := fmt.Sprintf("%s/v1/operator/pending-slo-changes/%s/%s", c.baseURL, changeID, action)

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("confirm %s: status %d", action, resp.StatusCode)
	}
	return nil
}

// --- queue endpoints ---

// GetDatadogConfig fetches per-tenant Datadog credentials from titlis-api.
// Credentials are provided by the API and used only within the current collection cycle.
func (c *Client) GetDatadogConfig(ctx context.Context) (*queue.DDCredentials, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/operator/datadog-config", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get datadog-config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("get datadog-config: status %d", resp.StatusCode)
	}

	var creds queue.DDCredentials
	if err := json.NewDecoder(resp.Body).Decode(&creds); err != nil {
		return nil, fmt.Errorf("decode datadog-config: %w", err)
	}
	return &creds, nil
}

// GetQueueConfig fetches queue monitoring feature configuration for the tenant.
func (c *Client) GetQueueConfig(ctx context.Context) (*queue.QueueConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/operator/queue-config", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get queue-config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("get queue-config: status %d", resp.StatusCode)
	}

	var cfg queue.QueueConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode queue-config: %w", err)
	}
	return &cfg, nil
}

// GetQueueNames fetches the known queue identifiers for the tenant so the env-var correlation
// scan can match them locally against Deployment env values (Fase 3).
func (c *Client) GetQueueNames(ctx context.Context) ([]queue.QueueName, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/operator/queue-names", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get queue-names: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("get queue-names: status %d", resp.StatusCode)
	}

	var names []queue.QueueName
	if err := json.NewDecoder(resp.Body).Decode(&names); err != nil {
		return nil, fmt.Errorf("decode queue-names: %w", err)
	}
	return names, nil
}

// SendQueueLinkHints posts fila↔workload matches found in the cluster. Fire-and-forget.
func (c *Client) SendQueueLinkHints(ctx context.Context, hints []queue.QueueLinkHint) {
	logger := log.FromContext(ctx)
	if len(hints) == 0 {
		return
	}

	b, err := json.Marshal(hints)
	if err != nil {
		logger.Error(err, "titlisapi: SendQueueLinkHints marshal failed")
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/operator/queue/link-hints", bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		logger.Error(err, "titlisapi: SendQueueLinkHints send failed")
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		logger.Info("titlisapi: SendQueueLinkHints unexpected status", "status", resp.StatusCode)
	}
}

// GetLabelRegistry fetches valid label values for the tenant's queue scoring.
func (c *Client) GetLabelRegistry(ctx context.Context) (queue.LabelRegistry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/operator/label-registry", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get label-registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("get label-registry: status %d", resp.StatusCode)
	}

	// Response: {"labels": [{"key": ..., "values": [...]}]}
	var wrapper struct {
		Labels []struct {
			Key    string   `json:"key"`
			Values []string `json:"values"`
		} `json:"labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("decode label-registry: %w", err)
	}

	registry := make(queue.LabelRegistry, len(wrapper.Labels))
	for _, e := range wrapper.Labels {
		registry[e.Key] = e.Values
	}
	return registry, nil
}

func observeBody(snap queue.QueueSnapshot) map[string]any {
	return map[string]any{
		"provider":               snap.Provider,
		"externalId":             snap.ExternalID,
		"displayName":            snap.DisplayName,
		"projectId":              snap.ProjectID,
		"topicId":                snap.TopicID,
		"isDlq":                  snap.IsDLQ,
		"numUndeliveredMessages": snap.NumUndeliveredMessages,
		"oldestUnackedAgeSec":    snap.OldestUnackedAgeSec,
		"pullMessageCountRate":   snap.PullMessageCountRate,
		"sendMessageCountRate":   snap.SendMessageCountRate,
		"ackMessageCountRate":    snap.AckMessageCountRate,
		"deadLetterMessageCount": snap.DeadLetterMessageCount,
		"hasDlqConfigured":       snap.HasDLQConfigured,
		"hasSnapshotPolicy":      snap.HasSnapshotPolicy,
		"hasMonitorBacklog":      snap.HasMonitorBacklog,
		"hasMonitorAge":          snap.HasMonitorAge,
		"hasMonitorDlq":          snap.HasMonitorDLQ,
		"labels":                 snap.Labels,
	}
}

// RecordQueueObservation sends a single queue snapshot observation to titlis-api.
// Fire-and-forget: failures are logged but do not block the runner.
func (c *Client) RecordQueueObservation(ctx context.Context, snap queue.QueueSnapshot) {
	logger := log.FromContext(ctx)

	b, err := json.Marshal(observeBody(snap))
	if err != nil {
		logger.Error(err, "titlisapi: RecordQueueObservation marshal failed")
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/operator/queue/observe", bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		logger.Error(err, "titlisapi: RecordQueueObservation send failed", "external_id", snap.ExternalID)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		logger.Info("titlisapi: RecordQueueObservation unexpected status",
			"status", resp.StatusCode, "external_id", snap.ExternalID)
	}
}

// RecordQueueObservationBatch sends all snapshots in a single HTTP call and returns
// lifecycle state for each, keyed by externalId. Eliminates N×2 requests per cycle.
func (c *Client) RecordQueueObservationBatch(ctx context.Context, snaps []queue.QueueSnapshot) (map[string]*queue.QueueLifecycle, error) {
	logger := log.FromContext(ctx)

	bodies := make([]map[string]any, 0, len(snaps))
	for _, snap := range snaps {
		bodies = append(bodies, observeBody(snap))
	}

	b, err := json.Marshal(bodies)
	if err != nil {
		return nil, fmt.Errorf("batch observe marshal: %w", err)
	}

	// Use a dedicated client with a longer timeout — processing 300+ items in one DB
	// transaction takes more time than the default per-request client timeout.
	batchClient := &http.Client{Timeout: 2 * time.Minute, Transport: c.http.Transport}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/operator/queue/observe/batch", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	logger.Info("titlisapi: batch observe sending", "count", len(snaps))
	resp, err := batchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("batch observe: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("batch observe: status %d", resp.StatusCode)
	}

	var items []struct {
		ExternalID       string `json:"externalId"`
		LifecycleState   string `json:"lifecycleState"`
		ObservationCount int    `json:"observationCount"`
		LearningTarget   int    `json:"learningTarget"`
		Thresholds       *struct {
			BacklogWarning  int64 `json:"backlogWarning"`
			BacklogCritical int64 `json:"backlogCritical"`
			AgeWarningSec   int64 `json:"ageWarningSec"`
			AgeCriticalSec  int64 `json:"ageCriticalSec"`
			P50Backlog      int64 `json:"p50Backlog"`
			P75Backlog      int64 `json:"p75Backlog"`
			P95Backlog      int64 `json:"p95Backlog"`
			P50AgeSec       int64 `json:"p50AgeSec"`
			P75AgeSec       int64 `json:"p75AgeSec"`
			P95AgeSec       int64 `json:"p95AgeSec"`
		} `json:"thresholds,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("batch observe decode: %w", err)
	}

	result := make(map[string]*queue.QueueLifecycle, len(items))
	for _, item := range items {
		lc := &queue.QueueLifecycle{
			State:            item.LifecycleState,
			ObservationCount: item.ObservationCount,
			LearningTarget:   item.LearningTarget,
		}
		if item.Thresholds != nil {
			lc.Thresholds = &queue.QueueThresholds{
				BacklogWarning:  item.Thresholds.BacklogWarning,
				BacklogCritical: item.Thresholds.BacklogCritical,
				AgeWarningSec:   item.Thresholds.AgeWarningSec,
				AgeCriticalSec:  item.Thresholds.AgeCriticalSec,
				P50Backlog:      item.Thresholds.P50Backlog,
				P75Backlog:      item.Thresholds.P75Backlog,
				P95Backlog:      item.Thresholds.P95Backlog,
				P50AgeSec:       item.Thresholds.P50AgeSec,
				P75AgeSec:       item.Thresholds.P75AgeSec,
				P95AgeSec:       item.Thresholds.P95AgeSec,
			}
		}
		result[item.ExternalID] = lc
	}

	logger.Info("titlisapi: batch observe complete", "count", len(result))
	return result, nil
}

// GetQueueLifecycle returns the current lifecycle state of a queue.
func (c *Client) GetQueueLifecycle(ctx context.Context, externalID, provider string) (*queue.QueueLifecycle, error) {
	u, _ := url.Parse(c.baseURL + "/v1/operator/queue/lifecycle")
	q := u.Query()
	q.Set("externalId", externalID)
	q.Set("provider", provider)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get queue lifecycle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &queue.QueueLifecycle{State: "DISCOVERING", ObservationCount: 0}, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("get queue lifecycle: status %d", resp.StatusCode)
	}

	// Map JSON response → queue.QueueLifecycle
	var raw struct {
		State            string `json:"state"`
		ObservationCount int    `json:"observationCount"`
		LearningTarget   int    `json:"learningTarget"`
		Thresholds       *struct {
			BacklogWarning  int64 `json:"backlogWarning"`
			BacklogCritical int64 `json:"backlogCritical"`
			AgeWarningSec   int64 `json:"ageWarningSec"`
			AgeCriticalSec  int64 `json:"ageCriticalSec"`
			P50Backlog      int64 `json:"p50Backlog"`
			P75Backlog      int64 `json:"p75Backlog"`
			P95Backlog      int64 `json:"p95Backlog"`
			P50AgeSec       int64 `json:"p50AgeSec"`
			P75AgeSec       int64 `json:"p75AgeSec"`
			P95AgeSec       int64 `json:"p95AgeSec"`
		} `json:"thresholds,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode queue lifecycle: %w", err)
	}

	lc := &queue.QueueLifecycle{
		State:            raw.State,
		ObservationCount: raw.ObservationCount,
		LearningTarget:   raw.LearningTarget,
	}
	if raw.Thresholds != nil {
		t := &queue.QueueThresholds{
			BacklogWarning:  raw.Thresholds.BacklogWarning,
			BacklogCritical: raw.Thresholds.BacklogCritical,
			AgeWarningSec:   raw.Thresholds.AgeWarningSec,
			AgeCriticalSec:  raw.Thresholds.AgeCriticalSec,
			P50Backlog:      raw.Thresholds.P50Backlog,
			P75Backlog:      raw.Thresholds.P75Backlog,
			P95Backlog:      raw.Thresholds.P95Backlog,
			P50AgeSec:       raw.Thresholds.P50AgeSec,
			P75AgeSec:       raw.Thresholds.P75AgeSec,
			P95AgeSec:       raw.Thresholds.P95AgeSec,
		}
		lc.Thresholds = t
	}
	return lc, nil
}

// PromoteQueueToMonitoring triggers threshold calculation and state transition for a queue.
// Fire-and-forget.
func (c *Client) PromoteQueueToMonitoring(ctx context.Context, externalID, provider string) {
	logger := log.FromContext(ctx)

	u, _ := url.Parse(c.baseURL + "/v1/operator/queue/promote")
	q := u.Query()
	q.Set("externalId", externalID)
	q.Set("provider", provider)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		logger.Error(err, "titlisapi: PromoteQueueToMonitoring failed", "external_id", externalID)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		logger.Info("titlisapi: PromoteQueueToMonitoring unexpected status",
			"status", resp.StatusCode, "external_id", externalID)
	}
}

// EvaluateQueue sends the queue snapshot to titlis-api, which proxies it to titlis-scoreops
// for scoring. Fire-and-forget.
func (c *Client) EvaluateQueue(
	ctx context.Context,
	snap queue.QueueSnapshot,
	thresholds queue.QueueThresholds,
	registry queue.LabelRegistry,
) {
	logger := log.FromContext(ctx)

	// Convert LabelRegistry (map[string][]string) to [{key, values}] as expected by the API.
	type labelKV struct {
		Key    string   `json:"key"`
		Values []string `json:"values"`
	}
	labelList := make([]labelKV, 0, len(registry))
	for k, v := range registry {
		labelList = append(labelList, labelKV{Key: k, Values: v})
	}

	body := map[string]any{
		"provider":               snap.Provider,
		"externalId":             snap.ExternalID,
		"displayName":            snap.DisplayName,
		"isDlq":                  snap.IsDLQ,
		"tenantId":               snap.TenantID,
		"numUndeliveredMessages": snap.NumUndeliveredMessages,
		"oldestUnackedAgeSec":    snap.OldestUnackedAgeSec,
		"pullMessageCountRate":   snap.PullMessageCountRate,
		"sendMessageCountRate":   snap.SendMessageCountRate,
		"ackMessageCountRate":    snap.AckMessageCountRate,
		"deadLetterMessageCount": snap.DeadLetterMessageCount,
		"hasDlqConfigured":       snap.HasDLQConfigured,
		"hasSnapshotPolicy":      snap.HasSnapshotPolicy,
		"hasMonitorBacklog":      snap.HasMonitorBacklog,
		"hasMonitorAge":          snap.HasMonitorAge,
		"hasMonitorDlq":          snap.HasMonitorDLQ,
		"labels":                 snap.Labels,
		"thresholds": map[string]any{
			"backlogWarning":  thresholds.BacklogWarning,
			"backlogCritical": thresholds.BacklogCritical,
			"ageWarningSec":   thresholds.AgeWarningSec,
			"ageCriticalSec":  thresholds.AgeCriticalSec,
			"p50Backlog":      thresholds.P50Backlog,
			"p75Backlog":      thresholds.P75Backlog,
			"p95Backlog":      thresholds.P95Backlog,
			"p50AgeSec":       thresholds.P50AgeSec,
			"p75AgeSec":       thresholds.P75AgeSec,
			"p95AgeSec":       thresholds.P95AgeSec,
		},
		"labelRegistry": labelList,
	}

	b, err := json.Marshal(body)
	if err != nil {
		logger.Error(err, "titlisapi: EvaluateQueue marshal failed")
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/operator/queue/evaluate", bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		logger.Error(err, "titlisapi: EvaluateQueue send failed", "external_id", snap.ExternalID)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		logger.Info("titlisapi: EvaluateQueue unexpected status",
			"status", resp.StatusCode, "external_id", snap.ExternalID)
	} else {
		logger.V(1).Info("titlisapi: EvaluateQueue sent", "external_id", snap.ExternalID)
	}
}

// --- payload builder helpers ---

func RemediationCategory(ruleID string) *string {
	var cat string
	switch ruleID {
	case "RES-007", "RES-008", "PERF-002":
		cat = "hpa"
	case "RES-003", "RES-004", "RES-005", "RES-006", "PERF-001":
		cat = "resources"
	default:
		return nil
	}
	return &cat
}

func ComplianceStatusStr(score float64) string {
	if score >= 90 {
		return "COMPLIANT"
	}
	return "NON_COMPLIANT"
}

func FilterAnnotations(annotations map[string]string) map[string]string {
	out := make(map[string]string, len(annotations))
	for k, v := range annotations {
		if !strings.HasPrefix(k, "kubectl.kubernetes.io/last-applied-configuration") {
			out[k] = v
		}
	}
	return out
}

func BuildScorecardPayload(sc *model.ResourceScorecard) ScorecardEvaluatedPayload {
	p := ScorecardEvaluatedPayload{
		WorkloadID:       sc.ResourceUID,
		Namespace:        sc.ResourceNamespace,
		Workload:         sc.ResourceName,
		OverallScore:     sc.OverallScore,
		ComplianceStatus: ComplianceStatusStr(sc.OverallScore),
		TotalRules:       sc.TotalChecks,
		PassedRules:      sc.PassedChecks,
		FailedRules:      sc.TotalChecks - sc.PassedChecks,
		CriticalFailures: sc.CriticalIssues,
		ErrorCount:       sc.ErrorIssues,
		WarningCount:     sc.WarningIssues,
		ScorecardVersion: 1,
		WorkloadKind:     sc.ResourceKind,
		EvaluatedAt:      sc.Timestamp,
	}

	for pillar, ps := range sc.PillarScores {
		p.PillarScores = append(p.PillarScores, PillarScorePayload{
			Pillar:        strings.ToUpper(string(pillar)),
			Score:         ps.Score,
			PassedChecks:  ps.PassedChecks,
			FailedChecks:  ps.TotalChecks - ps.PassedChecks,
			WeightedScore: ps.WeightedScore,
		})
		for _, r := range ps.ValidationResults {
			p.ValidationResults = append(p.ValidationResults, ValidationPayload{
				RuleID:              r.RuleID,
				RuleName:            r.RuleName,
				Pillar:              strings.ToUpper(string(r.Pillar)),
				Passed:              r.Passed,
				Severity:            strings.ToUpper(string(r.Severity)),
				RuleType:            strings.ToUpper(string(r.RuleType)),
				Weight:              r.Weight,
				Message:             r.Message,
				ActualValue:         r.ActualValue,
				IsRemediable:        r.IsRemediable,
				RemediationCategory: RemediationCategory(r.RuleID),
			})
		}
	}
	return p
}
