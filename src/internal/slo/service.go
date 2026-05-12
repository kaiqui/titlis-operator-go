package slo

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/api/v1alpha1"
	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/model"
)

// Provider — hoje Datadog, amanhã Grafana/outros.
type Provider interface {
	GetServiceSLOs(ctx context.Context, service string) ([]model.SLO, error)
	CreateSLO(ctx context.Context, slo model.SLO) (*model.SLO, error)
	UpdateSLO(ctx context.Context, id string, slo model.SLO) error
	FindSLOByTag(ctx context.Context, tag string) (*model.SLO, error)
}

// CatalogProvider — hoje Datadog Service Catalog, amanhã outro.
type CatalogProvider interface {
	GetServiceDefinition(ctx context.Context, service string) (*model.ServiceDefinition, error)
	DetectFramework(ctx context.Context, service string) (*model.SLOAppFramework, error)
}

type Service struct {
	provider Provider
	catalog  CatalogProvider
	settings *config.Settings
}

func NewService(provider Provider, catalog CatalogProvider, settings *config.Settings) *Service {
	return &Service{provider: provider, catalog: catalog, settings: settings}
}

func (s *Service) Reconcile(ctx context.Context, sloConfig *v1alpha1.SLOConfig) (*v1alpha1.SLOConfigStatus, error) {
	logger := log.FromContext(ctx).WithName("slo-service").WithValues("service", sloConfig.Spec.Service)

	spec := sloConfig.Spec
	uid := string(sloConfig.UID)
	now := metav1.Now()

	// PATH A — fast path: slo_id já está no status → verificar via tag
	if sloConfig.Status.SLOID != nil && *sloConfig.Status.SLOID != "" {
		existing, err := s.provider.FindSLOByTag(ctx, "titlis_resource_uid:"+uid)
		if err == nil && existing != nil {
			logger.V(1).Info("fast path: SLO found by tag", "slo_id", existing.ID)
			return s.checkAndUpdate(ctx, existing, spec, now)
		}
		logger.V(1).Info("fast path miss: SLO not found by tag, falling to orphan check", "status_slo_id", *sloConfig.Status.SLOID)
	}

	// PATH B — orphan safety: busca pelo tag sem depender do status
	existing, err := s.provider.FindSLOByTag(ctx, "titlis_resource_uid:"+uid)
	if err == nil && existing != nil {
		logger.V(1).Info("orphan path: SLO found by tag", "slo_id", existing.ID)
		return s.checkAndUpdate(ctx, existing, spec, now)
	}

	// PATH C — caminho normal: valida serviço no catálogo → cria SLO
	if s.settings.AutoSLORequireDatadogSvc {
		def, _ := s.catalog.GetServiceDefinition(ctx, spec.Service)
		if def == nil {
			logger.Info("service not found in Datadog catalog, skipping SLO creation")
			errMsg := fmt.Sprintf("serviço %q não encontrado no catálogo Datadog", spec.Service)
			return &v1alpha1.SLOConfigStatus{
				State: "error",
				Error: strPtr(errMsg),
			}, nil
		}
	}

	framework := s.detectFramework(ctx, sloConfig)
	sloToCreate := s.buildSLO(spec, uid, framework)

	created, err := s.provider.CreateSLO(ctx, sloToCreate)
	if err != nil {
		return nil, err
	}

	logger.Info("SLO created", "slo_id", created.ID, "target", spec.Target, "timeframe", spec.Timeframe)
	return &v1alpha1.SLOConfigStatus{
		SLOID:             &created.ID,
		State:             "synced",
		LastSync:          &now,
		DetectedFramework: (*string)(framework),
	}, nil
}

func (s *Service) checkAndUpdate(ctx context.Context, existing *model.SLO,
	spec v1alpha1.SLOConfigSpec, now metav1.Time) (*v1alpha1.SLOConfigStatus, error) {

	logger := log.FromContext(ctx).WithName("slo-service").WithValues("service", spec.Service, "slo_id", existing.ID)

	// Update if target or warning changed
	needsUpdate := existing.TargetThreshold != spec.Target
	if spec.Warning != nil && (existing.WarningThreshold == nil || *existing.WarningThreshold != *spec.Warning) {
		needsUpdate = true
	}

	if needsUpdate {
		logger.Info("SLO out of sync, updating",
			"old_target", existing.TargetThreshold, "new_target", spec.Target)
		uid := ""
		for _, t := range existing.Tags {
			if strings.HasPrefix(t, "titlis_resource_uid:") {
				uid = strings.TrimPrefix(t, "titlis_resource_uid:")
			}
		}
		framework := model.SLOAppFramework(spec.Type)
		updated := s.buildSLO(spec, uid, &framework)
		if err := s.provider.UpdateSLO(ctx, existing.ID, updated); err != nil {
			return nil, err
		}
		logger.Info("SLO updated", "target", spec.Target)
	} else {
		logger.V(1).Info("SLO in sync, no update needed", "target", existing.TargetThreshold)
	}

	return &v1alpha1.SLOConfigStatus{
		SLOID:    &existing.ID,
		State:    "synced",
		LastSync: &now,
	}, nil
}

func (s *Service) detectFramework(ctx context.Context, sloConfig *v1alpha1.SLOConfig) *model.SLOAppFramework {
	// 1. spec.app_framework (explícito)
	if sloConfig.Spec.AppFramework != nil {
		fw := model.SLOAppFramework(*sloConfig.Spec.AppFramework)
		return &fw
	}

	// 2. annotation metadata["titlis.io/app-framework"]
	if ann, ok := sloConfig.Annotations["titlis.io/app-framework"]; ok && ann != "" {
		fw := model.SLOAppFramework(ann)
		return &fw
	}

	// 3. Datadog ServiceDefinition tags "framework:*"
	if fw, err := s.catalog.DetectFramework(ctx, sloConfig.Spec.Service); err == nil && fw != nil {
		return fw
	}

	// 4. Fallback: wsgi
	fw := model.FrameworkWSGI
	return &fw
}

func (s *Service) buildSLO(spec v1alpha1.SLOConfigSpec, uid string,
	framework *model.SLOAppFramework) model.SLO {

	tags := append([]string{
		"managed_by:titlis_operator",
		"titlis_resource_uid:" + uid,
	}, spec.Tags...)

	slo := model.SLO{
		ServiceName:     spec.Service,
		SLOType:         model.SLOType(spec.Type),
		TargetThreshold: spec.Target,
		WarningThreshold: spec.Warning,
		Timeframe:       model.SLOTimeframe(spec.Timeframe),
		Tags:            tags,
		Numerator:       spec.Numerator,
		Denominator:     spec.Denominator,
	}

	if slo.SLOType == "" {
		slo.SLOType = model.SLOTypeMetric
	}
	if slo.Timeframe == "" {
		slo.Timeframe = model.SLOTimeframe30d
	}

	fwName := "wsgi"
	if framework != nil {
		fwName = string(*framework)
	}
	slo.Name = fmt.Sprintf("%s-%s-slo", spec.Service, fwName)

	return slo
}

func extractEnvFromSpec(spec v1alpha1.SLOConfigSpec, labels map[string]string) string {
	for _, tag := range spec.Tags {
		if strings.HasPrefix(tag, "env:") {
			return strings.TrimPrefix(tag, "env:")
		}
	}
	if env, ok := labels["titlis.io/dd-env"]; ok && env != "" {
		return env
	}
	return "production"
}

func strPtr(s string) *string { return &s }
