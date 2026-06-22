package discovery_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/titlis/operator/internal/discovery"
)

func TestServiceCorrelator_LinksByUSTag(t *testing.T) {
	assets := []discovery.Asset{
		{ExternalID: "service:orders", Provider: "datadog", Kind: "dd_service", Name: "orders"},
		{ExternalID: "dep-1", Provider: "kubernetes", Kind: "deployment", Name: "orders-api",
			Attributes: map[string]any{"ddService": "orders"}},
	}

	rels := discovery.ServiceCorrelator{}.Correlate(assets)

	assert.Len(t, rels, 1)
	assert.Equal(t, "service:orders", rels[0].SourceExternalID)
	assert.Equal(t, "datadog", rels[0].SourceProvider)
	assert.Equal(t, "dep-1", rels[0].TargetExternalID)
	assert.Equal(t, "kubernetes", rels[0].TargetProvider)
	assert.Equal(t, "describes", rels[0].Type)
}

func TestServiceCorrelator_FallbackExactName(t *testing.T) {
	assets := []discovery.Asset{
		{ExternalID: "service:payments", Provider: "datadog", Kind: "dd_service", Name: "payments"},
		{ExternalID: "ss-1", Provider: "kubernetes", Kind: "statefulset", Name: "payments"},
	}

	rels := discovery.ServiceCorrelator{}.Correlate(assets)

	assert.Len(t, rels, 1)
	assert.Equal(t, "service:payments", rels[0].SourceExternalID)
	assert.Equal(t, "ss-1", rels[0].TargetExternalID)
}

func TestServiceCorrelator_NoMatchNoEdge(t *testing.T) {
	assets := []discovery.Asset{
		{ExternalID: "service:orders", Provider: "datadog", Kind: "dd_service", Name: "orders"},
		{ExternalID: "dep-1", Provider: "kubernetes", Kind: "deployment", Name: "billing-api",
			Attributes: map[string]any{"ddService": "ghost"}},
		// non-workload k8s asset must be ignored
		{ExternalID: "svc-1", Provider: "kubernetes", Kind: "service", Name: "orders"},
	}

	rels := discovery.ServiceCorrelator{}.Correlate(assets)

	assert.Empty(t, rels)
}

func TestServiceCorrelator_NoDatadogServicesReturnsNil(t *testing.T) {
	assets := []discovery.Asset{
		{ExternalID: "dep-1", Provider: "kubernetes", Kind: "deployment", Name: "orders-api"},
	}
	assert.Nil(t, discovery.ServiceCorrelator{}.Correlate(assets))
}

func TestRegistry_RunsCorrelatorsAfterMerge(t *testing.T) {
	k8s := &fakeProvider{name: "kubernetes", enabled: true, sub: discovery.AssetSubgraph{
		Assets: []discovery.Asset{{ExternalID: "dep-1", Provider: "kubernetes", Kind: "deployment",
			Name: "orders-api", Attributes: map[string]any{"ddService": "orders"}}},
	}}
	dd := &fakeProvider{name: "datadog", enabled: true, sub: discovery.AssetSubgraph{
		Assets: []discovery.Asset{{ExternalID: "service:orders", Provider: "datadog", Kind: "dd_service", Name: "orders"}},
	}}

	snap := discovery.NewRegistry(k8s, dd).
		WithCorrelators(discovery.ServiceCorrelator{}).
		DiscoverAll(context.Background(), "prod-k8s")

	assert.Len(t, snap.Assets, 2)
	assert.Len(t, snap.Relations, 1)
	assert.Equal(t, "describes", snap.Relations[0].Type)
}
