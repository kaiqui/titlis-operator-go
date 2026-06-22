package discovery_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/titlis/operator/internal/discovery"
)

type fakeProvider struct {
	name    string
	enabled bool
	sub     discovery.AssetSubgraph
	err     error
}

func (f *fakeProvider) Name() string  { return f.name }
func (f *fakeProvider) Enabled() bool { return f.enabled }
func (f *fakeProvider) Discover(_ context.Context) (discovery.AssetSubgraph, error) {
	return f.sub, f.err
}

func TestRegistry_MergesEnabledProviders(t *testing.T) {
	a := &fakeProvider{name: "kubernetes", enabled: true, sub: discovery.AssetSubgraph{
		Assets:    []discovery.Asset{{ExternalID: "u1", Provider: "kubernetes", Kind: "deployment"}},
		Relations: []discovery.Relation{{SourceExternalID: "u1", TargetExternalID: "u2", Type: "selects"}},
	}}
	b := &fakeProvider{name: "datadog", enabled: true, sub: discovery.AssetSubgraph{
		Assets: []discovery.Asset{{ExternalID: "m1", Provider: "datadog", Kind: "dd_monitor"}},
	}}

	snap := discovery.NewRegistry(a, b).DiscoverAll(context.Background(), "prod-k8s")

	assert.Equal(t, 1, snap.V)
	assert.Equal(t, "prod-k8s", snap.Cluster)
	assert.Len(t, snap.Assets, 2)
	assert.Len(t, snap.Relations, 1)
	assert.Equal(t, discovery.StatusOK, snap.SyncStatus["kubernetes"].Status)
	assert.Equal(t, 1, snap.SyncStatus["kubernetes"].AssetCount)
	assert.Equal(t, discovery.StatusOK, snap.SyncStatus["datadog"].Status)
}

func TestRegistry_NotConfiguredProviderIsSkippedNotFailed(t *testing.T) {
	dd := &fakeProvider{name: "datadog", enabled: false}
	k8s := &fakeProvider{name: "kubernetes", enabled: true, sub: discovery.AssetSubgraph{
		Assets: []discovery.Asset{{ExternalID: "u1", Kind: "deployment"}},
	}}

	snap := discovery.NewRegistry(k8s, dd).DiscoverAll(context.Background(), "c")

	assert.Len(t, snap.Assets, 1)
	assert.Equal(t, discovery.StatusNotConfigured, snap.SyncStatus["datadog"].Status)
	assert.Empty(t, snap.SyncStatus["datadog"].Error)
}

func TestRegistry_ProviderErrorDoesNotAbortOthers(t *testing.T) {
	bad := &fakeProvider{name: "datadog", enabled: true, err: errors.New("rate limited")}
	good := &fakeProvider{name: "kubernetes", enabled: true, sub: discovery.AssetSubgraph{
		Assets: []discovery.Asset{{ExternalID: "u1", Kind: "deployment"}},
	}}

	snap := discovery.NewRegistry(bad, good).DiscoverAll(context.Background(), "c")

	assert.Len(t, snap.Assets, 1)
	assert.Equal(t, discovery.StatusError, snap.SyncStatus["datadog"].Status)
	assert.Equal(t, "rate limited", snap.SyncStatus["datadog"].Error)
}
