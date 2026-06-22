package discovery

// Provider status values reported per source in an AssetGraphSnapshot.
const (
	StatusOK            = "ok"
	StatusPartial       = "partial"
	StatusError         = "error"
	StatusNotConfigured = "not_configured"
)

// Asset is a single discovered entity, normalized across providers. The shape is intentionally
// generic so new providers (datadog, dynatrace, otel, cloud) need no schema change — the
// provider-specific shape lives in Attributes.
type Asset struct {
	ExternalID string            `json:"externalId"` // natural key within the provider (k8s UID, dd monitor id, ...)
	Provider   string            `json:"provider"`   // kubernetes | datadog | ...
	Kind       string            `json:"kind"`       // deployment | service | dd_monitor | ...
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace,omitempty"`
	Cluster    string            `json:"cluster,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	Attributes map[string]any    `json:"attributes,omitempty"`
}

// Relation is a directed edge between two assets, keyed by their natural (provider, externalId).
// titlis-api resolves these to surrogate ids on ingestion.
type Relation struct {
	SourceExternalID string `json:"sourceExternalId"`
	SourceProvider   string `json:"sourceProvider"`
	TargetExternalID string `json:"targetExternalId"`
	TargetProvider   string `json:"targetProvider"`
	Type             string `json:"type"`
}

// ProviderStatus reports how a single provider's discovery went in a sweep. A provider that is
// not connected reports StatusNotConfigured — never an error and never zero coverage.
type ProviderStatus struct {
	Status     string `json:"status"`
	AssetCount int    `json:"assetCount"`
	Error      string `json:"error,omitempty"`
}

// AssetSubgraph is what a single provider returns from Discover.
type AssetSubgraph struct {
	Assets    []Asset
	Relations []Relation
	Status    ProviderStatus
}

// AssetGraphSnapshot is the merged, cross-provider graph shipped to titlis-api each sweep.
type AssetGraphSnapshot struct {
	V          int                       `json:"v"`
	Cluster    string                    `json:"cluster"`
	Assets     []Asset                   `json:"assets"`
	Relations  []Relation                `json:"relations"`
	SyncStatus map[string]ProviderStatus `json:"syncStatus"`
}
