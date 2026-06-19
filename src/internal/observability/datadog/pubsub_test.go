package datadog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubName_FullPath(t *testing.T) {
	assert.Equal(t, "my-sub", subName("projects/my-project/subscriptions/my-sub"))
}

func TestSubName_ShortName(t *testing.T) {
	assert.Equal(t, "my-sub", subName("my-sub"))
}

func TestSubName_EmptyString(t *testing.T) {
	assert.Equal(t, "", subName(""))
}

func TestExtractTagValue_Found(t *testing.T) {
	tags := []string{"env:production", "subscription_id:projects/proj/subscriptions/my-sub", "team:platform"}
	assert.Equal(t, "projects/proj/subscriptions/my-sub", extractTagValue(tags, "subscription_id"))
}

func TestExtractTagValue_NotFound(t *testing.T) {
	tags := []string{"env:production"}
	assert.Equal(t, "", extractTagValue(tags, "subscription_id"))
}

func TestExtractTagValue_EmptyTags(t *testing.T) {
	assert.Equal(t, "", extractTagValue(nil, "subscription_id"))
}

func TestExtractScopeValue_Found(t *testing.T) {
	scope := "subscription_id:projects/proj/subscriptions/my-sub,env:production"
	assert.Equal(t, "projects/proj/subscriptions/my-sub", extractScopeValue(scope, "subscription_id"))
}

func TestExtractScopeValue_NotFound(t *testing.T) {
	scope := "env:production"
	assert.Equal(t, "", extractScopeValue(scope, "subscription_id"))
}

func TestParseTags_MultipleEntries(t *testing.T) {
	tags := []string{"env:production", "team:platform", "project_id:my-proj"}
	result := parseTags(tags)
	assert.Equal(t, "production", result["env"])
	assert.Equal(t, "platform", result["team"])
	assert.Equal(t, "my-proj", result["project_id"])
}

func TestParseTags_ValueWithColon(t *testing.T) {
	tags := []string{"subscription_id:projects/proj/subscriptions/my-sub"}
	result := parseTags(tags)
	assert.Equal(t, "projects/proj/subscriptions/my-sub", result["subscription_id"])
}

func TestParseTags_MalformedTag(t *testing.T) {
	tags := []string{"no-colon-here", "env:production"}
	result := parseTags(tags)
	assert.Equal(t, "production", result["env"])
	_, hasBad := result["no-colon-here"]
	assert.False(t, hasBad, "malformed tag without colon should be skipped")
}

func TestIsDLQDetection(t *testing.T) {
	cases := []struct {
		externalID string
		wantDLQ    bool
	}{
		{"projects/proj/subscriptions/orders-dlq", true},
		{"projects/proj/subscriptions/orders-DLQ", true},
		{"projects/proj/subscriptions/orders-sub", false},
		{"projects/proj/subscriptions/dlq-orders", true},   // prefix
		{"projects/proj/subscriptions/my-dlq-sub", true},   // middle
		{"projects/proj/subscriptions/orders-dlq-retry", true}, // suffix variant
		{"my-queue-dlq", true},
		{"normal-subscription", false},
	}

	for _, tc := range cases {
		displayName := subName(tc.externalID)
		isDLQ := strings.Contains(strings.ToLower(displayName), "dlq")
		assert.Equal(t, tc.wantDLQ, isDLQ, "external_id=%s", tc.externalID)
	}
}

func TestHasDLQConfiguredFlagging(t *testing.T) {
	// Reproduce the CollectAll post-processing logic in pubsub.go
	type subEntry struct {
		projectID string
		topicID   string
		isDLQ     bool
		hasDLQ    bool
	}

	subs := []subEntry{
		{projectID: "proj", topicID: "orders-topic", isDLQ: false},
		{projectID: "proj", topicID: "orders-topic", isDLQ: true},
		{projectID: "proj", topicID: "payments-topic", isDLQ: false},
	}

	dlqSet := make(map[string]bool)
	for _, s := range subs {
		if s.isDLQ {
			dlqSet[s.projectID+"/"+s.topicID] = true
		}
	}
	for i := range subs {
		if !subs[i].isDLQ {
			subs[i].hasDLQ = dlqSet[subs[i].projectID+"/"+subs[i].topicID]
		}
	}

	assert.True(t, subs[0].hasDLQ, "orders subscription should have HasDLQConfigured=true")
	assert.False(t, subs[1].hasDLQ, "DLQ subscription itself is not marked")
	assert.False(t, subs[2].hasDLQ, "payments subscription has no DLQ counterpart")
}
