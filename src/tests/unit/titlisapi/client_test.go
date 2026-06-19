package titlisapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/model"
	"github.com/titlis/operator/internal/titlisapi"
)

func newTestServer(t *testing.T) (*httptest.Server, *[]map[string]any) {
	received := &[]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		json.Unmarshal(body, &payload)
		*received = append(*received, payload)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, received
}

func newClient(baseURL string) *titlisapi.Client {
	return titlisapi.New(&config.Settings{
		TitlisAPIEnabled:        true,
		TitlisAPIHost:           "localhost",
		TitlisAPIHTTPPort:       8080,
		TitlisAPIKey:            "tk_test",
		TitlisAPITimeoutSeconds: 5,
	})
}

func TestClient_SendScorecardEvaluated_EnvelopeFields(t *testing.T) {
	srv, received := newTestServer(t)

	cfg := config.Settings{
		TitlisAPIEnabled:        true,
		TitlisAPITimeoutSeconds: 5,
	}
	_ = cfg

	// Override base URL to test server
	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)
	payload := titlisapi.ScorecardEvaluatedPayload{
		WorkloadID:       "uid-123",
		Namespace:        "production",
		Workload:         "my-api",
		OverallScore:     87.5,
		ComplianceStatus: "NON_COMPLIANT",
		ScorecardVersion: 1,
		EvaluatedAt:      time.Now(),
	}
	client.SendScorecardEvaluated(context.Background(), payload)

	require.Len(t, *received, 1)
	env := (*received)[0]

	assert.Equal(t, float64(1), env["v"], "envelope version must be 1")
	assert.Equal(t, "scorecard_evaluated", env["t"])
	assert.Equal(t, "tk_test", env["api_key"])
	assert.NotNil(t, env["ts"])
	assert.NotNil(t, env["data"])
}

func TestClient_BuildScorecardPayload_UPPERCASE(t *testing.T) {
	sc := &model.ResourceScorecard{
		ResourceName:      "my-api",
		ResourceNamespace: "production",
		ResourceUID:       "uid-123",
		OverallScore:      95.0,
		Timestamp:         time.Now(),
		PillarScores: map[model.ValidationPillar]model.PillarScore{
			model.PillarResilience: {
				Pillar:       model.PillarResilience,
				Score:        100.0,
				PassedChecks: 5,
				TotalChecks:  5,
				ValidationResults: []model.ValidationResult{
					{
						RuleID:   "RES-001",
						RuleName: "Liveness Probe",
						Pillar:   model.PillarResilience,
						Passed:   true,
						Severity: model.SeverityError,
						RuleType: model.RuleTypeBoolean,
						Weight:   10.0,
						Message:  "✅ ok",
					},
				},
			},
		},
	}

	p := titlisapi.BuildScorecardPayload(sc)

	assert.Equal(t, "COMPLIANT", p.ComplianceStatus, "score=95 → COMPLIANT")
	require.Len(t, p.PillarScores, 1)
	assert.Equal(t, "RESILIENCE", p.PillarScores[0].Pillar, "pillar must be UPPERCASE")
	require.Len(t, p.ValidationResults, 1)
	assert.Equal(t, "RESILIENCE", p.ValidationResults[0].Pillar)
	assert.Equal(t, "ERROR", p.ValidationResults[0].Severity)
	assert.Equal(t, "BOOLEAN", p.ValidationResults[0].RuleType)
}

func TestClient_FilterAnnotations_RemovesKubectl(t *testing.T) {
	annotations := map[string]string{
		"kubectl.kubernetes.io/last-applied-configuration": "big-json",
		"app.kubernetes.io/version":                        "1.0.0",
		"titlis.io/auto-created":                           "true",
	}
	filtered := titlisapi.FilterAnnotations(annotations)
	assert.NotContains(t, filtered, "kubectl.kubernetes.io/last-applied-configuration")
	assert.Contains(t, filtered, "app.kubernetes.io/version")
	assert.Contains(t, filtered, "titlis.io/auto-created")
}

func TestClient_RemediationCategory(t *testing.T) {
	assert.Equal(t, "hpa", *titlisapi.RemediationCategory("RES-007"))
	assert.Equal(t, "hpa", *titlisapi.RemediationCategory("RES-008"))
	assert.Equal(t, "hpa", *titlisapi.RemediationCategory("PERF-002"))
	assert.Equal(t, "resources", *titlisapi.RemediationCategory("RES-003"))
	assert.Equal(t, "resources", *titlisapi.RemediationCategory("PERF-001"))
	assert.Nil(t, titlisapi.RemediationCategory("OPS-001"))
	assert.Nil(t, titlisapi.RemediationCategory("SEC-001"))
}

func TestClient_ComplianceStatusStr(t *testing.T) {
	assert.Equal(t, "COMPLIANT", titlisapi.ComplianceStatusStr(90.0))
	assert.Equal(t, "COMPLIANT", titlisapi.ComplianceStatusStr(100.0))
	assert.Equal(t, "NON_COMPLIANT", titlisapi.ComplianceStatusStr(89.9))
	assert.Equal(t, "NON_COMPLIANT", titlisapi.ComplianceStatusStr(0.0))
}

func TestClient_New_CreatesClient(t *testing.T) {
	cfg := &config.Settings{
		TitlisAPIEnabled:        true,
		TitlisAPIHost:           "titlis-api.svc",
		TitlisAPIHTTPPort:       8080,
		TitlisAPIKey:            "tk_abc",
		TitlisAPITimeoutSeconds: 10,
	}
	c := titlisapi.New(cfg)
	assert.NotNil(t, c)
}

func TestClient_EvaluateWorkload_SendsPost(t *testing.T) {
	srv, received := newTestServerWith(t, http.StatusAccepted)

	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)
	client.EvaluateWorkload(context.Background(), map[string]string{"uid": "uid-1", "name": "my-svc"})

	require.Len(t, *received, 1)
	assert.Equal(t, "uid-1", (*received)[0]["uid"])
}

func TestClient_SendRemediationEvent_Fires(t *testing.T) {
	srv, received := newTestServer(t)
	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)

	client.SendRemediationEvent(context.Background(), titlisapi.RemediationEventPayload{
		PRNumber: 42, Status: "merged",
	})

	require.Len(t, *received, 1)
	assert.Equal(t, "remediation_updated", (*received)[0]["t"])
}

func TestClient_SendSLOReconciled_Fires(t *testing.T) {
	srv, received := newTestServer(t)
	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)

	client.SendSLOReconciled(context.Background(), titlisapi.SLOReconciledPayload{
		SLOID: "slo-1", Service: "my-svc", Target: 99.9, State: "OK",
	})

	require.Len(t, *received, 1)
	assert.Equal(t, "slo_reconciled", (*received)[0]["t"])
}

func TestClient_SendNotificationLog_Fires(t *testing.T) {
	srv, received := newTestServer(t)
	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)

	client.SendNotificationLog(context.Background(), titlisapi.NotificationLogPayload{
		Severity: "warning", Message: "test", Channel: "#alerts",
	})

	require.Len(t, *received, 1)
	assert.Equal(t, "notification_sent", (*received)[0]["t"])
}

func TestClient_SendResourceMetrics_Fires(t *testing.T) {
	srv, received := newTestServer(t)
	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)

	client.SendResourceMetrics(context.Background(), titlisapi.ResourceMetricsPayload{
		CPUMillicores: 100, MemoryMiB: 256, Namespace: "production",
	})

	require.Len(t, *received, 1)
	assert.Equal(t, "resource_metrics", (*received)[0]["t"])
}

func TestClient_SendNamespaceExclusionsSync_Fires(t *testing.T) {
	srv, received := newTestServer(t)
	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)

	client.SendNamespaceExclusionsSync(context.Background(), titlisapi.NamespaceExclusionsSyncPayload{
		Cluster: "prod", ExcludedNamespaces: []string{"kube-system"},
	})

	require.Len(t, *received, 1)
	assert.Equal(t, "namespace_exclusions_sync", (*received)[0]["t"])
}

func TestClient_SendServiceDefinitionSynced_Fires(t *testing.T) {
	srv, received := newTestServer(t)
	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)

	client.SendServiceDefinitionSynced(context.Background(), titlisapi.ServiceDefinitionSyncedPayload{
		ServiceName: "platform-svc", Team: "platform", Workloads: []string{"deploy-1"},
	})

	require.Len(t, *received, 1)
	assert.Equal(t, "service_definition_synced", (*received)[0]["t"])
}

func TestClient_GetGitHubToken_ReturnsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "tk_test", r.Header.Get("X-Api-Key"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"githubToken":"ghp_abc","githubBaseBranch":"main"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)
	token, err := client.GetGitHubToken(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "ghp_abc", token)
}

func TestClient_GetGitHubToken_ErrorOnNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)
	_, err := client.GetGitHubToken(context.Background())

	assert.Error(t, err)
}

func TestClient_GetPendingSLOChanges_ReturnsList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":"ch-1","slo_config_name":"my-slo","field":"target","old_value":"99","new_value":"99.9"}]`)) //nolint:errcheck
	}))
	defer srv.Close()

	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)
	changes, err := client.GetPendingSLOChanges(context.Background())

	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, "ch-1", changes[0].ID)
}

func TestClient_GetPendingSLOChanges_NotFound_ReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)
	changes, err := client.GetPendingSLOChanges(context.Background())

	require.NoError(t, err)
	assert.Nil(t, changes)
}

func TestClient_GetPendingSLOChanges_ErrorOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)
	_, err := client.GetPendingSLOChanges(context.Background())

	assert.Error(t, err)
}

func TestClient_ConfirmSLOChangeApplied_PostsCorrectPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)
	err := client.ConfirmSLOChangeApplied(context.Background(), "change-123")

	require.NoError(t, err)
	assert.Equal(t, "/v1/operator/pending-slo-changes/change-123/applied", gotPath)
}

func TestClient_ConfirmSLOChangeFailed_PostsErrorBody(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := titlisapi.NewWithBaseURL(srv.URL, "tk_test", 5*time.Second)
	err := client.ConfirmSLOChangeFailed(context.Background(), "change-456", "apply failed")

	require.NoError(t, err)
	assert.Equal(t, "apply failed", gotBody["error"])
}

// newTestServerWith creates a test server that always returns the given status code.
func newTestServerWith(t *testing.T, status int) (*httptest.Server, *[]map[string]any) {
	received := &[]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		json.Unmarshal(body, &payload) //nolint:errcheck
		*received = append(*received, payload)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, received
}
