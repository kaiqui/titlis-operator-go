package otel

import (
	"context"

	"github.com/titlis/operator/internal/discovery"
)

const providerName = "otel"

// Options configures the OpenTelemetry discovery provider.
type Options struct {
	Enabled  bool
	Endpoint string // collector/backend query endpoint; empty → not configured
}

// Provider is a scaffold that proves the discovery.Provider contract is drop-in: a new source only
// implements the interface and registers in the Registry — the DiscoveryRunner and the HTTP contract
// are untouched. The actual mapping of OTel resources / service graph to assets lands in a later
// phase; until an endpoint is configured the provider reports not_configured (graceful degradation).
type Provider struct {
	opts Options
}

func New(opts Options) *Provider { return &Provider{opts: opts} }

func (p *Provider) Name() string  { return providerName }
func (p *Provider) Enabled() bool { return p.opts.Enabled }

func (p *Provider) Discover(_ context.Context) (discovery.AssetSubgraph, error) {
	if p.opts.Endpoint == "" {
		return discovery.AssetSubgraph{
			Status: discovery.ProviderStatus{Status: discovery.StatusNotConfigured},
		}, nil
	}
	// TODO(D5+): consultar o collector/backend OTel pelo service graph + resource attributes e mapear
	// para assets (kind "otel_service") + relações, no mesmo shape do DatadogProvider. Enquanto não
	// implementado, reporta ok-vazio quando há endpoint para não mascarar configuração válida.
	return discovery.AssetSubgraph{
		Status: discovery.ProviderStatus{Status: discovery.StatusOK},
	}, nil
}
