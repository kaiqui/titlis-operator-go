package datadog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyMetric(t *testing.T) {
	assert.Equal(t, "jvm", classifyMetric("jvm.heap_memory"))
	assert.Equal(t, "http", classifyMetric("http.request.duration"))
	assert.Equal(t, "http", classifyMetric("trace.servlet.request.hits"))
	assert.Equal(t, "messaging", classifyMetric("kafka.consumer.lag"))
	assert.Equal(t, "database", classifyMetric("postgresql.connections"))
	assert.Equal(t, "infra", classifyMetric("system.cpu.user"))
	assert.Equal(t, "", classifyMetric("custom.business.metric"))
}

func TestMetricCapabilities(t *testing.T) {
	cats, caps := metricCapabilities([]string{
		"jvm.heap_memory", "http.request.duration", "trace.servlet.request.hits", "custom.x",
	})
	assert.Equal(t, []string{"http", "jvm"}, cats) // ordenado
	assert.Contains(t, caps, "metrics")
	assert.Contains(t, caps, "tracing")

	cats2, caps2 := metricCapabilities(nil)
	assert.Empty(t, cats2)
	assert.Empty(t, caps2)
}
