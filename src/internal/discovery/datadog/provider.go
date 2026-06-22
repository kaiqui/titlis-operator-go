package datadog

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	datadogV1 "github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
	datadogV2 "github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"

	"github.com/titlis/operator/internal/discovery"
	"github.com/titlis/operator/internal/queue"
)

const (
	providerName = "datadog"
	metricsCap   = 5000
)

// credsFetcher fetches per-tenant Datadog credentials from titlis-api. Implemented by
// titlisapi.Client.GetDatadogConfig. Creds are used only within the current sweep — never logged,
// never persisted (regra 15).
type credsFetcher interface {
	GetDatadogConfig(ctx context.Context) (*queue.DDCredentials, error)
}

// Options configures the Datadog discovery provider.
type Options struct {
	Enabled        bool
	IncludeMetrics bool          // active-metric discovery is expensive — off by default
	MetricsWindow  time.Duration // how far back ListActiveMetrics looks
	PageLimit      int           // safety cap on pages per resource
}

// Provider discovers Datadog assets (services, monitors, SLOs, metrics) into the shared graph.
// First external impl of discovery.Provider — Dynatrace/OTel follow the same shape.
type Provider struct {
	api  credsFetcher
	opts Options
}

func New(api credsFetcher, opts Options) *Provider {
	if opts.PageLimit <= 0 {
		opts.PageLimit = 50
	}
	if opts.MetricsWindow <= 0 {
		opts.MetricsWindow = time.Hour
	}
	return &Provider{api: api, opts: opts}
}

func (p *Provider) Name() string  { return providerName }
func (p *Provider) Enabled() bool { return p.opts.Enabled }

func (p *Provider) Discover(ctx context.Context) (discovery.AssetSubgraph, error) {
	creds, err := p.api.GetDatadogConfig(ctx)
	if err != nil {
		return discovery.AssetSubgraph{Status: discovery.ProviderStatus{Status: discovery.StatusError, Error: err.Error()}}, nil
	}
	if creds == nil || creds.APIKey == "" || creds.AppKey == "" {
		// Graceful degradation (§3.9): Datadog não conectado → N/A, nunca falha.
		return discovery.AssetSubgraph{Status: discovery.ProviderStatus{Status: discovery.StatusNotConfigured}}, nil
	}

	client, authCtx := newClient(ctx, *creds)
	b := newBuilder()

	p.discoverServices(authCtx, client, b)
	p.discoverMonitors(authCtx, client, b)
	p.discoverSLOs(authCtx, client, b)
	if p.opts.IncludeMetrics {
		p.discoverMetrics(authCtx, client, b)
	}

	status := discovery.ProviderStatus{Status: discovery.StatusOK}
	if len(b.errors) > 0 {
		status.Status = discovery.StatusPartial
		status.Error = strings.Join(b.errors, "; ")
	}
	return discovery.AssetSubgraph{Assets: b.assets, Relations: b.relations, Status: status}, nil
}

// --- discovery steps ---

func (p *Provider) discoverServices(ctx context.Context, client *datadog.APIClient, b *builder) {
	api := datadogV2.NewServiceDefinitionApi(client)
	pageSize := int64(100)
	for page := int64(0); page < int64(p.opts.PageLimit); page++ {
		resp, _, err := api.ListServiceDefinitions(ctx,
			*datadogV2.NewListServiceDefinitionsOptionalParameters().
				WithSchemaVersion(datadogV2.SERVICEDEFINITIONSCHEMAVERSIONS_V2_2).
				WithPageSize(pageSize).WithPageNumber(page))
		if err != nil {
			b.fail("services", err)
			return
		}
		data := resp.GetData()
		for i := range data {
			name := serviceName(&data[i])
			attrs := map[string]any{"definitionId": data[i].GetId()}
			// Opção B: enriquece o dd_service com capacidades mensuráveis (métricas/tracing) via uma
			// consulta por serviço. Gated (caro). Logs ficam de fora (Logs API) → N/A honesto.
			if p.opts.IncludeMetrics {
				cats, caps := p.serviceMetricCapabilities(ctx, client, name)
				if len(cats) > 0 {
					attrs["metricCategories"] = cats
				}
				if len(caps) > 0 {
					attrs["capabilities"] = caps
				}
			}
			b.add(discovery.Asset{
				ExternalID: "service:" + name, Provider: providerName, Kind: "dd_service",
				Name:       name,
				Attributes: attrs,
			})
		}
		if len(data) < int(pageSize) {
			return
		}
	}
}

func (p *Provider) discoverMonitors(ctx context.Context, client *datadog.APIClient, b *builder) {
	api := datadogV1.NewMonitorsApi(client)
	pageSize := int32(1000)
	for page := int64(0); page < int64(p.opts.PageLimit); page++ {
		monitors, _, err := api.ListMonitors(ctx,
			*datadogV1.NewListMonitorsOptionalParameters().WithPage(page).WithPageSize(pageSize))
		if err != nil {
			b.fail("monitors", err)
			return
		}
		for i := range monitors {
			m := &monitors[i]
			ext := "monitor:" + strconv.FormatInt(m.GetId(), 10)
			b.monitorExists[ext] = true
			b.add(discovery.Asset{
				ExternalID: ext, Provider: providerName, Kind: "dd_monitor",
				Name:       m.GetName(),
				Tags:       tagSliceToMap(m.GetTags()),
				Attributes: map[string]any{"type": string(m.GetType()), "monitorId": m.GetId()},
			})
			// D5a: liga o monitor ao serviço via tag `service:`. A edge é descartada na ingestão se
			// o dd_service correspondente não existir no grafo.
			for _, svc := range serviceTags(m.GetTags()) {
				b.rel(ext, "service:"+svc, "monitors")
			}
		}
		if len(monitors) < int(pageSize) {
			return
		}
	}
}

func (p *Provider) discoverSLOs(ctx context.Context, client *datadog.APIClient, b *builder) {
	api := datadogV1.NewServiceLevelObjectivesApi(client)
	limit := int64(1000)
	for page := int64(0); page < int64(p.opts.PageLimit); page++ {
		resp, _, err := api.ListSLOs(ctx,
			*datadogV1.NewListSLOsOptionalParameters().WithLimit(limit).WithOffset(page*limit))
		if err != nil {
			b.fail("slos", err)
			return
		}
		data := resp.GetData()
		for i := range data {
			s := &data[i]
			ext := "slo:" + s.GetId()
			b.add(discovery.Asset{
				ExternalID: ext, Provider: providerName, Kind: "dd_slo",
				Name:       s.GetName(),
				Tags:       tagSliceToMap(s.GetTags()),
				Attributes: map[string]any{"type": string(s.GetType())},
			})
			// D5a: liga o SLO ao serviço via tag `service:` (descartada na ingestão se ausente).
			for _, svc := range serviceTags(s.GetTags()) {
				b.rel(ext, "service:"+svc, "targets")
			}
			// dd_slo based_on dd_monitor (só quando o monitor foi descoberto neste sweep).
			for _, mid := range s.GetMonitorIds() {
				target := "monitor:" + strconv.FormatInt(mid, 10)
				if b.monitorExists[target] {
					b.rel(ext, target, "based_on")
				}
			}
		}
		if len(data) < int(limit) {
			return
		}
	}
}

func (p *Provider) discoverMetrics(ctx context.Context, client *datadog.APIClient, b *builder) {
	api := datadogV1.NewMetricsApi(client)
	from := time.Now().Add(-p.opts.MetricsWindow).Unix()
	resp, _, err := api.ListActiveMetrics(ctx, from)
	if err != nil {
		b.fail("metrics", err)
		return
	}
	names := resp.GetMetrics()
	if len(names) > metricsCap {
		b.errors = append(b.errors, fmt.Sprintf("metrics: %d active, capped at %d", len(names), metricsCap))
		names = names[:metricsCap]
	}
	for _, n := range names {
		b.add(discovery.Asset{
			ExternalID: "metric:" + n, Provider: providerName, Kind: "metric", Name: n,
		})
	}
}

// --- helpers ---

func newClient(ctx context.Context, creds queue.DDCredentials) (*datadog.APIClient, context.Context) {
	site := creds.Site
	if site == "" {
		site = "datadoghq.com"
	}
	cfg := datadog.NewConfiguration()
	cfg.Host = "api." + site
	cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	cfg.RetryConfiguration.EnableRetry = true
	cfg.RetryConfiguration.MaxRetries = 3
	client := datadog.NewAPIClient(cfg)
	authCtx := context.WithValue(ctx, datadog.ContextAPIKeys, map[string]datadog.APIKey{
		"apiKeyAuth": {Key: creds.APIKey},
		"appKeyAuth": {Key: creds.AppKey},
	})
	return client, authCtx
}

func serviceName(d *datadogV2.ServiceDefinitionData) string {
	attrs := d.GetAttributes()
	if attrs.Schema != nil {
		if s := attrs.Schema.ServiceDefinitionV2Dot2; s != nil {
			if n := s.GetDdService(); n != "" {
				return n
			}
		}
		if s := attrs.Schema.ServiceDefinitionV2Dot1; s != nil {
			if n := s.GetDdService(); n != "" {
				return n
			}
		}
	}
	return d.GetId()
}

// serviceTags extracts the `service:` tag values from a Datadog tag slice (used to link
// monitors/SLOs to their dd_service in the graph). A resource may carry more than one.
func serviceTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		if v, ok := strings.CutPrefix(t, "service:"); ok && v != "" {
			out = append(out, v)
		}
	}
	return out
}

// tagSliceToMap turns Datadog "key:value" tags into a map. Tags without a colon become key="".
func tagSliceToMap(tags []string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, t := range tags {
		if i := strings.IndexByte(t, ':'); i >= 0 {
			out[t[:i]] = t[i+1:]
		} else {
			out[t] = ""
		}
	}
	return out
}
