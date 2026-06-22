package discovery_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes/scheme"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/titlis/operator/internal/discovery"
	dddiscovery "github.com/titlis/operator/internal/discovery/datadog"
	k8sdiscovery "github.com/titlis/operator/internal/discovery/kubernetes"
	oteldiscovery "github.com/titlis/operator/internal/discovery/otel"
)

// assertProviderContract checks the invariants every discovery.Provider must honor (doc.go).
// Reuse this when adding a new provider — it must pass without changes to the runner or contract.
func assertProviderContract(t *testing.T, p discovery.Provider) {
	t.Helper()
	assert.NotEmpty(t, p.Name(), "Name() must be stable and non-empty")

	sub, err := p.Discover(context.Background())
	assert.NoError(t, err, "Discover should encode failures in Status, not return err")

	validStatus := map[string]bool{
		"": true, // Registry defaults empty → ok
		discovery.StatusOK: true, discovery.StatusPartial: true,
		discovery.StatusError: true, discovery.StatusNotConfigured: true,
	}
	assert.True(t, validStatus[sub.Status.Status], "unknown status %q", sub.Status.Status)

	for _, a := range sub.Assets {
		assert.NotEmpty(t, a.ExternalID, "asset externalId")
		assert.NotEmpty(t, a.Kind, "asset kind")
		assert.NotEmpty(t, a.Name, "asset name")
		assert.Equal(t, p.Name(), a.Provider, "asset.Provider must equal Name()")
	}
	for _, r := range sub.Relations {
		assert.NotEmpty(t, r.SourceExternalID)
		assert.NotEmpty(t, r.TargetExternalID)
		assert.NotEmpty(t, r.Type)
		assert.NotEmpty(t, r.SourceProvider)
		assert.NotEmpty(t, r.TargetProvider)
	}

	// Deterministic for the same input.
	sub2, _ := p.Discover(context.Background())
	assert.Equal(t, len(sub.Assets), len(sub2.Assets), "Discover must be deterministic for same input")
}

func TestProviderContract_AllProviders(t *testing.T) {
	emptyCluster := ctrlfake.NewClientBuilder().WithScheme(scheme.Scheme).Build()

	providers := []discovery.Provider{
		k8sdiscovery.New(emptyCluster, stubExcluder{excluded: map[string]bool{}}, "c"),
		dddiscovery.New(fakeCreds{creds: nil}, dddiscovery.Options{Enabled: true}),
		oteldiscovery.New(oteldiscovery.Options{Enabled: true}),
	}

	for _, p := range providers {
		t.Run(p.Name(), func(t *testing.T) { assertProviderContract(t, p) })
	}
}

func TestOtelProvider_DropIn(t *testing.T) {
	// Not configured (no endpoint) → not_configured, zero assets, no error.
	notConfigured := oteldiscovery.New(oteldiscovery.Options{Enabled: true})
	sub, err := notConfigured.Discover(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, discovery.StatusNotConfigured, sub.Status.Status)
	assert.Empty(t, sub.Assets)

	// With an endpoint the scaffold reports ok (placeholder until implemented).
	configured := oteldiscovery.New(oteldiscovery.Options{Enabled: true, Endpoint: "http://collector:4317"})
	sub2, _ := configured.Discover(context.Background())
	assert.Equal(t, discovery.StatusOK, sub2.Status.Status)

	assert.False(t, oteldiscovery.New(oteldiscovery.Options{Enabled: false}).Enabled())
	assert.Equal(t, "otel", notConfigured.Name())
}

// TestRegistry_DropInNewProviderNoContractChange proves a brand-new provider flows through the
// Registry (merge + status) with no change to the runner or the snapshot contract.
func TestRegistry_DropInNewProviderNoContractChange(t *testing.T) {
	snap := discovery.NewRegistry(
		oteldiscovery.New(oteldiscovery.Options{Enabled: true}),
	).DiscoverAll(context.Background(), "c")

	assert.Equal(t, discovery.StatusNotConfigured, snap.SyncStatus["otel"].Status)
	assert.Empty(t, snap.Assets)
}
