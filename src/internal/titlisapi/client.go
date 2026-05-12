package titlisapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/model"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	timeout time.Duration
}

func New(cfg *config.Settings) *Client {
	timeout := time.Duration(cfg.TitlisAPITimeoutSeconds) * time.Second
	return &Client{
		baseURL: cfg.TitlisAPIBaseURL(),
		apiKey:  cfg.TitlisAPIKey,
		http:    &http.Client{Timeout: timeout},
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
