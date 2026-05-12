package datadog

import (
	"context"
	"fmt"
	"strings"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	datadogV1 "github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
	datadogV2 "github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"

	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/model"
)

// DatadogSLO implicitly satisfies slo.Provider and slo.CatalogProvider.
type DatadogSLO struct {
	slosAPI    *datadogV1.ServiceLevelObjectivesApi
	catalogAPI *datadogV2.ServiceDefinitionApi
	authCtx    context.Context
}

func NewSLOClient(cfg *config.Settings) *DatadogSLO {
	configuration := datadog.NewConfiguration()
	configuration.Host = "api." + cfg.DatadogSite
	apiClient := datadog.NewAPIClient(configuration)

	authCtx := context.WithValue(context.Background(), datadog.ContextAPIKeys, map[string]datadog.APIKey{
		"apiKeyAuth": {Key: cfg.DatadogAPIKey},
		"appKeyAuth": {Key: cfg.DatadogAppKey},
	})

	return &DatadogSLO{
		slosAPI:    datadogV1.NewServiceLevelObjectivesApi(apiClient),
		catalogAPI: datadogV2.NewServiceDefinitionApi(apiClient),
		authCtx:    authCtx,
	}
}

func (d *DatadogSLO) GetServiceSLOs(_ context.Context, service string) ([]model.SLO, error) {
	resp, _, err := d.slosAPI.ListSLOs(d.authCtx, *datadogV1.NewListSLOsOptionalParameters().
		WithQuery("service:" + service))
	if err != nil {
		return nil, err
	}
	var out []model.SLO
	for _, s := range resp.GetData() {
		out = append(out, ddSLOToModel(s))
	}
	return out, nil
}

func (d *DatadogSLO) CreateSLO(_ context.Context, slo model.SLO) (*model.SLO, error) {
	target := datadogV1.SLOThreshold{
		Target: slo.TargetThreshold,
		Timeframe: datadogV1.SLOTimeframe(slo.Timeframe),
	}
	if slo.WarningThreshold != nil {
		target.SetWarning(*slo.WarningThreshold)
	}

	body := datadogV1.ServiceLevelObjectiveRequest{
		Name:       slo.Name,
		Type:       datadogV1.SLOType(slo.SLOType),
		Thresholds: []datadogV1.SLOThreshold{target},
		Tags:       slo.Tags,
	}
	if slo.Numerator != nil && slo.Denominator != nil {
		body.SetQuery(datadogV1.ServiceLevelObjectiveQuery{
			Numerator:   *slo.Numerator,
			Denominator: *slo.Denominator,
		})
	}

	resp, _, err := d.slosAPI.CreateSLO(d.authCtx, body)
	if err != nil {
		return nil, err
	}
	if len(resp.GetData()) == 0 {
		return nil, fmt.Errorf("CreateSLO: empty response")
	}
	created := ddSLOToModel(resp.GetData()[0])
	return &created, nil
}

func (d *DatadogSLO) UpdateSLO(_ context.Context, id string, slo model.SLO) error {
	target := datadogV1.SLOThreshold{
		Target:    slo.TargetThreshold,
		Timeframe: datadogV1.SLOTimeframe(slo.Timeframe),
	}
	if slo.WarningThreshold != nil {
		target.SetWarning(*slo.WarningThreshold)
	}
	body := datadogV1.ServiceLevelObjective{
		Name:       slo.Name,
		Thresholds: []datadogV1.SLOThreshold{target},
		Tags:       slo.Tags,
	}
	_, _, err := d.slosAPI.UpdateSLO(d.authCtx, id, body)
	return err
}

func (d *DatadogSLO) FindSLOByTag(_ context.Context, tag string) (*model.SLO, error) {
	resp, _, err := d.slosAPI.ListSLOs(d.authCtx, *datadogV1.NewListSLOsOptionalParameters().
		WithTagsQuery(tag))
	if err != nil {
		return nil, err
	}
	data := resp.GetData()
	if len(data) == 0 {
		return nil, nil
	}
	s := ddSLOToModel(data[0])
	return &s, nil
}

func (d *DatadogSLO) GetServiceDefinition(_ context.Context, service string) (*model.ServiceDefinition, error) {
	resp, _, err := d.catalogAPI.GetServiceDefinition(d.authCtx, service,
		*datadogV2.NewGetServiceDefinitionOptionalParameters())
	if err != nil {
		return nil, err
	}

	def := &model.ServiceDefinition{DDService: service}
	tags, team := extractServiceTags(resp)
	def.Tags = tags
	if team != "" {
		def.Team = &team
	}
	return def, nil
}

func (d *DatadogSLO) DetectFramework(_ context.Context, service string) (*model.SLOAppFramework, error) {
	resp, _, err := d.catalogAPI.GetServiceDefinition(d.authCtx, service,
		*datadogV2.NewGetServiceDefinitionOptionalParameters())
	if err != nil {
		return nil, err
	}

	tags, _ := extractServiceTags(resp)
	for _, tag := range tags {
		if strings.HasPrefix(tag, "framework:") {
			fw := model.SLOAppFramework(strings.TrimPrefix(tag, "framework:"))
			return &fw, nil
		}
	}
	return nil, nil
}

// extractServiceTags extracts tags and team from a ServiceDefinitionGetResponse,
// handling schema versions V1, V2, V2.1 and V2.2 transparently.
func extractServiceTags(resp datadogV2.ServiceDefinitionGetResponse) (tags []string, team string) {
	data := resp.GetData()
	attrs := data.GetAttributes()
	schema := attrs.GetSchema()
	actual := schema.GetActualInstance()
	switch v := actual.(type) {
	case *datadogV2.ServiceDefinitionV2:
		tags = v.GetTags()
		team = v.GetTeam()
	case *datadogV2.ServiceDefinitionV2Dot1:
		tags = v.GetTags()
		team = v.GetTeam()
	case *datadogV2.ServiceDefinitionV2Dot2:
		tags = v.GetTags()
		team = v.GetTeam()
	case *datadogV2.ServiceDefinitionV1:
		tags = v.GetTags()
	}
	return
}

// --- helpers ---

func ddSLOToModel(s datadogV1.ServiceLevelObjective) model.SLO {
	slo := model.SLO{
		ID:          s.GetId(),
		Name:        s.GetName(),
		SLOType:     model.SLOType(s.GetType()),
		Tags:        s.GetTags(),
	}
	for _, t := range s.GetThresholds() {
		slo.TargetThreshold = t.GetTarget()
		slo.Timeframe = model.SLOTimeframe(t.GetTimeframe())
		if w := t.GetWarning(); w != 0 {
			slo.WarningThreshold = &w
		}
		break
	}
	for _, tag := range slo.Tags {
		if strings.HasPrefix(tag, "service:") {
			slo.ServiceName = strings.TrimPrefix(tag, "service:")
		}
	}
	return slo
}
