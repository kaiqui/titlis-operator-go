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

	"github.com/titlis/operator/internal/queue"
	"github.com/titlis/operator/internal/titlisapi"
)

func newQueueServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func newQueueClient(baseURL string) *titlisapi.Client {
	return titlisapi.NewWithBaseURL(baseURL, "tk_test", 5*time.Second)
}

// --- GetDatadogConfig ---

func TestGetDatadogConfig_ReturnsCreds(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "tk_test", r.Header.Get("X-Api-Key"))
		assert.Equal(t, "/v1/operator/datadog-config", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"ddApiKey": "api-key-123",
			"ddAppKey": "app-key-456",
			"ddSite":   "datadoghq.eu",
		})
	})

	client := newQueueClient(srv.URL)
	creds, err := client.GetDatadogConfig(context.Background())

	require.NoError(t, err)
	require.NotNil(t, creds)
	assert.Equal(t, "api-key-123", creds.APIKey)
	assert.Equal(t, "app-key-456", creds.AppKey)
	assert.Equal(t, "datadoghq.eu", creds.Site)
}

func TestGetDatadogConfig_Returns404AsNil(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	client := newQueueClient(srv.URL)
	creds, err := client.GetDatadogConfig(context.Background())

	require.NoError(t, err)
	assert.Nil(t, creds)
}

func TestGetDatadogConfig_ErrorOn5xx(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	client := newQueueClient(srv.URL)
	_, err := client.GetDatadogConfig(context.Background())
	require.Error(t, err)
}

// --- GetQueueConfig ---

func TestGetQueueConfig_Enabled(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/operator/queue-config", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"enabled":        true,
			"learningCycles": 5,
			"providers":      []string{"gcp_pubsub"},
		})
	})

	client := newQueueClient(srv.URL)
	cfg, err := client.GetQueueConfig(context.Background())

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 5, cfg.LearningCycles)
	assert.Contains(t, cfg.Providers, "gcp_pubsub")
}

func TestGetQueueConfig_Disabled(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"enabled": false})
	})

	client := newQueueClient(srv.URL)
	cfg, err := client.GetQueueConfig(context.Background())

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.False(t, cfg.Enabled)
}

// --- GetLabelRegistry ---

func TestGetLabelRegistry_DecodesWrappedResponse(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "tk_test", r.Header.Get("X-Api-Key"))
		assert.Equal(t, "/v1/operator/label-registry", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		// API returns {"labels": [{"key": ..., "values": [...]}]}
		json.NewEncoder(w).Encode(map[string]any{
			"labels": []map[string]any{
				{"key": "env", "values": []string{"production", "staging"}},
				{"key": "team", "values": []string{"platform"}},
			},
		})
	})

	client := newQueueClient(srv.URL)
	registry, err := client.GetLabelRegistry(context.Background())

	require.NoError(t, err)
	assert.Contains(t, registry["env"], "production")
	assert.Contains(t, registry["env"], "staging")
	assert.Contains(t, registry["team"], "platform")
}

func TestGetLabelRegistry_EmptyRegistry(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"labels": []any{}})
	})

	client := newQueueClient(srv.URL)
	registry, err := client.GetLabelRegistry(context.Background())

	require.NoError(t, err)
	assert.Empty(t, registry)
}

// --- RecordQueueObservation ---

func TestRecordQueueObservationBatch_ReturnsLifecycleMap(t *testing.T) {
	var received []map[string]any
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/operator/queue/observe/batch", r.URL.Path)
		assert.Equal(t, "tk_test", r.Header.Get("X-Api-Key"))
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"externalId": "projects/p/subscriptions/sub-1", "queueId": 1, "lifecycleState": "DISCOVERING", "observationCount": 1, "learningTarget": 7},
			{"externalId": "projects/p/subscriptions/sub-2", "queueId": 2, "lifecycleState": "LEARNING", "observationCount": 4, "learningTarget": 7},
		})
	})

	client := newQueueClient(srv.URL)
	snaps := []queue.QueueSnapshot{
		{Provider: "gcp_pubsub", ExternalID: "projects/p/subscriptions/sub-1", DisplayName: "sub-1"},
		{Provider: "gcp_pubsub", ExternalID: "projects/p/subscriptions/sub-2", DisplayName: "sub-2"},
	}

	result, err := client.RecordQueueObservationBatch(context.Background(), snaps)

	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Len(t, received, 2, "request body must contain all snapshots")
	assert.Equal(t, "DISCOVERING", result["projects/p/subscriptions/sub-1"].State)
	assert.Equal(t, 1, result["projects/p/subscriptions/sub-1"].ObservationCount)
	assert.Equal(t, "LEARNING", result["projects/p/subscriptions/sub-2"].State)
	assert.Equal(t, 4, result["projects/p/subscriptions/sub-2"].ObservationCount)
}

func TestRecordQueueObservationBatch_MonitoringStateIncludesThresholds(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"externalId": "sub-mon", "queueId": 3, "lifecycleState": "MONITORING",
				"observationCount": 10, "learningTarget": 7,
				"thresholds": map[string]any{
					"backlogWarning": 120, "backlogCritical": 150,
					"ageWarningSec": 60, "ageCriticalSec": 90,
					"p50Backlog": 80, "p75Backlog": 100, "p95Backlog": 110,
					"p50AgeSec": 30, "p75AgeSec": 50, "p95AgeSec": 60,
				},
			},
		})
	})

	client := newQueueClient(srv.URL)
	snaps := []queue.QueueSnapshot{{Provider: "gcp_pubsub", ExternalID: "sub-mon", DisplayName: "sub-mon"}}

	result, err := client.RecordQueueObservationBatch(context.Background(), snaps)

	require.NoError(t, err)
	require.NotNil(t, result["sub-mon"])
	lc := result["sub-mon"]
	assert.Equal(t, "MONITORING", lc.State)
	require.NotNil(t, lc.Thresholds)
	assert.Equal(t, int64(120), lc.Thresholds.BacklogWarning)
	assert.Equal(t, int64(150), lc.Thresholds.BacklogCritical)
	assert.Equal(t, int64(80), lc.Thresholds.P50Backlog)
}

func TestRecordQueueObservationBatch_ErrorOnServerFailure(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	client := newQueueClient(srv.URL)
	_, err := client.RecordQueueObservationBatch(context.Background(), []queue.QueueSnapshot{
		{Provider: "gcp_pubsub", ExternalID: "sub-1", DisplayName: "sub-1"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestRecordQueueObservation_SendsBody(t *testing.T) {
	var received map[string]any
	done := make(chan struct{})
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/operator/queue/observe", r.URL.Path)
		assert.Equal(t, "tk_test", r.Header.Get("X-Api-Key"))
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
		close(done)
	})

	client := newQueueClient(srv.URL)
	snap := queue.QueueSnapshot{
		Provider:               "gcp_pubsub",
		ExternalID:             "projects/proj/subscriptions/my-sub",
		DisplayName:            "my-sub",
		ProjectID:              "proj",
		NumUndeliveredMessages: 42,
		OldestUnackedAgeSec:    10,
	}
	client.RecordQueueObservation(context.Background(), snap)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for observation")
	}

	require.NotNil(t, received)
	assert.Equal(t, "gcp_pubsub", received["provider"])
	assert.Equal(t, "projects/proj/subscriptions/my-sub", received["externalId"])
	assert.Equal(t, float64(42), received["numUndeliveredMessages"])
}

func TestRecordQueueObservation_ServerError_DoesNotPanic(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	client := newQueueClient(srv.URL)
	client.RecordQueueObservation(context.Background(), queue.QueueSnapshot{
		Provider:   "gcp_pubsub",
		ExternalID: "projects/proj/subscriptions/test",
	})
	// fire-and-forget — just ensure no panic/deadlock
	time.Sleep(100 * time.Millisecond)
}

// --- GetQueueLifecycle ---

func TestGetQueueLifecycle_LearningState(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/operator/queue/lifecycle", r.URL.Path)
		assert.Equal(t, "projects/proj/subscriptions/my-sub", r.URL.Query().Get("externalId"))
		assert.Equal(t, "gcp_pubsub", r.URL.Query().Get("provider"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state":            "LEARNING",
			"observationCount": 4,
			"learningTarget":   7,
		})
	})

	client := newQueueClient(srv.URL)
	lc, err := client.GetQueueLifecycle(context.Background(), "projects/proj/subscriptions/my-sub", "gcp_pubsub")

	require.NoError(t, err)
	require.NotNil(t, lc)
	assert.Equal(t, "LEARNING", lc.State)
	assert.Equal(t, 4, lc.ObservationCount)
	assert.Equal(t, 7, lc.LearningTarget)
	assert.Nil(t, lc.Thresholds)
}

func TestGetQueueLifecycle_MonitoringStateWithThresholds(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state":            "MONITORING",
			"observationCount": 10,
			"thresholds": map[string]any{
				"backlogWarning":  int64(120),
				"backlogCritical": int64(150),
				"ageWarningSec":   int64(60),
				"ageCriticalSec":  int64(90),
				"p50Backlog":      int64(80),
				"p75Backlog":      int64(100),
				"p95Backlog":      int64(110),
				"p50AgeSec":       int64(30),
				"p75AgeSec":       int64(50),
				"p95AgeSec":       int64(60),
			},
		})
	})

	client := newQueueClient(srv.URL)
	lc, err := client.GetQueueLifecycle(context.Background(), "my-sub", "gcp_pubsub")

	require.NoError(t, err)
	require.NotNil(t, lc)
	assert.Equal(t, "MONITORING", lc.State)
	require.NotNil(t, lc.Thresholds)
	assert.Equal(t, int64(120), lc.Thresholds.BacklogWarning)
	assert.Equal(t, int64(150), lc.Thresholds.BacklogCritical)
	assert.Equal(t, int64(60), lc.Thresholds.AgeWarningSec)
	assert.Equal(t, int64(80), lc.Thresholds.P50Backlog)
}

func TestGetQueueLifecycle_NotFound_ReturnsDiscovering(t *testing.T) {
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	client := newQueueClient(srv.URL)
	lc, err := client.GetQueueLifecycle(context.Background(), "unknown-sub", "gcp_pubsub")

	require.NoError(t, err)
	require.NotNil(t, lc)
	assert.Equal(t, "DISCOVERING", lc.State)
	assert.Equal(t, 0, lc.ObservationCount)
}

// --- PromoteQueueToMonitoring ---

func TestPromoteQueueToMonitoring_SendsCorrectRequest(t *testing.T) {
	done := make(chan struct{})
	var gotPath, gotMethod, gotProvider, gotExternalID string
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotExternalID = r.URL.Query().Get("externalId")
		gotProvider = r.URL.Query().Get("provider")
		w.WriteHeader(http.StatusOK)
		close(done)
	})

	client := newQueueClient(srv.URL)
	client.PromoteQueueToMonitoring(context.Background(), "projects/proj/subscriptions/my-sub", "gcp_pubsub")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for promote request")
	}

	assert.Equal(t, "/v1/operator/queue/promote", gotPath)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "projects/proj/subscriptions/my-sub", gotExternalID)
	assert.Equal(t, "gcp_pubsub", gotProvider)
}

// --- EvaluateQueue ---

func TestEvaluateQueue_SendsThresholdsAndRegistry(t *testing.T) {
	var received map[string]any
	done := make(chan struct{})
	srv := newQueueServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/operator/queue/evaluate", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusAccepted)
		close(done)
	})

	client := newQueueClient(srv.URL)
	snap := queue.QueueSnapshot{
		Provider:               "gcp_pubsub",
		ExternalID:             "projects/proj/subscriptions/my-sub",
		DisplayName:            "my-sub",
		NumUndeliveredMessages: 55,
		HasMonitorBacklog:      true,
	}
	thresholds := queue.QueueThresholds{
		BacklogWarning:  100,
		BacklogCritical: 150,
		AgeWarningSec:   60,
		AgeCriticalSec:  90,
	}
	registry := queue.LabelRegistry{
		"env":  {"production"},
		"team": {"platform"},
	}

	client.EvaluateQueue(context.Background(), snap, thresholds, registry)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for evaluate request")
	}

	require.NotNil(t, received)
	assert.Equal(t, "gcp_pubsub", received["provider"])
	assert.Equal(t, "projects/proj/subscriptions/my-sub", received["externalId"])

	thresholdsMap, ok := received["thresholds"].(map[string]any)
	require.True(t, ok, "thresholds field must be a map")
	assert.Equal(t, float64(100), thresholdsMap["backlogWarning"])
	assert.Equal(t, float64(150), thresholdsMap["backlogCritical"])

	labelReg, ok := received["labelRegistry"].([]any)
	require.True(t, ok, "labelRegistry field must be an array")
	found := false
	for _, entry := range labelReg {
		m, _ := entry.(map[string]any)
		if m["key"] == "env" {
			vals, _ := m["values"].([]any)
			assert.Contains(t, vals, "production")
			found = true
		}
	}
	assert.True(t, found, "env entry not found in labelRegistry")
}
