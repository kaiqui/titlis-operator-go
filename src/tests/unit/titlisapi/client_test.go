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
