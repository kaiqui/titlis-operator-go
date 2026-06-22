package discovery

import "context"

// Provider is a single discovery source. Datadog is the first external impl; Dynatrace/OTel/cloud
// are drop-in — they only implement this interface and register in the Registry, never touching
// the runner or the HTTP contract.
type Provider interface {
	Name() string
	Enabled() bool
	Discover(ctx context.Context) (AssetSubgraph, error)
}

// AssetSink receives the merged graph. Implemented by titlisapi.Client (fire-and-forget HTTP).
type AssetSink interface {
	SendAssetGraph(ctx context.Context, snap AssetGraphSnapshot)
}

// Registry merges the subgraphs of all registered providers into one snapshot, then runs any
// correlators to add cross-provider edges.
type Registry struct {
	providers   []Provider
	correlators []Correlator
}

func NewRegistry(providers ...Provider) *Registry {
	return &Registry{providers: providers}
}

// WithCorrelators registers cross-provider correlators run after all providers complete.
func (r *Registry) WithCorrelators(correlators ...Correlator) *Registry {
	r.correlators = correlators
	return r
}

func (r *Registry) DiscoverAll(ctx context.Context, cluster string) AssetGraphSnapshot {
	snap := AssetGraphSnapshot{
		V:          1,
		Cluster:    cluster,
		SyncStatus: make(map[string]ProviderStatus, len(r.providers)),
	}
	for _, p := range r.providers {
		if !p.Enabled() {
			snap.SyncStatus[p.Name()] = ProviderStatus{Status: StatusNotConfigured}
			continue
		}
		sub, err := p.Discover(ctx)
		if err != nil {
			snap.SyncStatus[p.Name()] = ProviderStatus{Status: StatusError, Error: err.Error()}
			continue
		}
		snap.Assets = append(snap.Assets, sub.Assets...)
		snap.Relations = append(snap.Relations, sub.Relations...)
		st := sub.Status
		if st.Status == "" {
			st.Status = StatusOK
		}
		st.AssetCount = len(sub.Assets)
		snap.SyncStatus[p.Name()] = st
	}
	for _, c := range r.correlators {
		snap.Relations = append(snap.Relations, c.Correlate(snap.Assets)...)
	}
	return snap
}
