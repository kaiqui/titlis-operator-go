package datadog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTagSliceToMap(t *testing.T) {
	got := tagSliceToMap([]string{"team:payments", "env:prod", "managed_by_titlis"})
	assert.Equal(t, "payments", got["team"])
	assert.Equal(t, "prod", got["env"])
	assert.Equal(t, "", got["managed_by_titlis"])

	assert.Nil(t, tagSliceToMap(nil))
}

func TestTagSliceToMap_ValueWithColon(t *testing.T) {
	got := tagSliceToMap([]string{"url:https://x.com/path"})
	assert.Equal(t, "https://x.com/path", got["url"])
}

func TestServiceTags(t *testing.T) {
	got := serviceTags([]string{"service:orders", "env:prod", "service:orders-worker", "team:pag", "service:"})
	assert.Equal(t, []string{"orders", "orders-worker"}, got)
	assert.Nil(t, serviceTags([]string{"env:prod"}))
}
