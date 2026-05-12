package notification_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/titlis/operator/internal/model"
	"github.com/titlis/operator/internal/notification"
)

func sc(name string, score float64) model.ResourceScorecard {
	return model.ResourceScorecard{ResourceName: name, OverallScore: score}
}

func TestBuffer_FlushOnBatchSize(t *testing.T) {
	buf := notification.NewNamespaceBuffer(60, 3) // flush at 3 items

	assert.Nil(t, buf.Add("ns", sc("a", 90)))
	assert.Nil(t, buf.Add("ns", sc("b", 80)))
	flush := buf.Add("ns", sc("c", 70))

	require.NotNil(t, flush, "should flush when batch size reached")
	assert.Len(t, flush, 3)
	assert.Equal(t, "a", flush[0].ResourceName)
}

func TestBuffer_FlushOnInterval(t *testing.T) {
	// Use 0-minute interval so it always flushes
	buf := notification.NewNamespaceBuffer(0, 100)

	// Wait a tiny bit so lastFlush is zero and interval has passed
	time.Sleep(1 * time.Millisecond)

	flush := buf.Add("ns", sc("x", 95))
	require.NotNil(t, flush, "should flush when interval elapsed")
	assert.Len(t, flush, 1)
}

func TestBuffer_DifferentNamespacesIndependent(t *testing.T) {
	buf := notification.NewNamespaceBuffer(60, 3) // flush at 3 items

	assert.Nil(t, buf.Add("ns-a", sc("a1", 90)))
	assert.Nil(t, buf.Add("ns-b", sc("b1", 80)))
	assert.Nil(t, buf.Add("ns-a", sc("a2", 70)))

	// ns-a reaches batch size at a3
	flushA := buf.Add("ns-a", sc("a3", 60))
	require.NotNil(t, flushA, "ns-a should flush at batch size 3")
	assert.Len(t, flushA, 3)

	// ns-b still has only 1 item
	assert.Nil(t, buf.Add("ns-b", sc("b2", 55)))
	flushB := buf.Add("ns-b", sc("b3", 50))
	require.NotNil(t, flushB, "ns-b should flush at batch size 3")
	assert.Len(t, flushB, 3)
}

func TestBuffer_ResetAfterFlush(t *testing.T) {
	buf := notification.NewNamespaceBuffer(60, 2)

	buf.Add("ns", sc("a", 90))
	buf.Add("ns", sc("b", 80)) // flushes

	// After flush, bucket is empty — next item shouldn't trigger flush
	result := buf.Add("ns", sc("c", 70))
	assert.Nil(t, result, "bucket was reset after flush; single item should not flush")
}
