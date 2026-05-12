package slo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/titlis/operator/api/v1alpha1"
	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/model"
	"github.com/titlis/operator/internal/slo"
)

// --- fakes ---

type fakeProvider struct {
	foundByTag *model.SLO
	created    *model.SLO
	createErr  error
	updateErr  error
	tagErr     error
}

func (f *fakeProvider) GetServiceSLOs(_ context.Context, _ string) ([]model.SLO, error) {
	return nil, nil
}
func (f *fakeProvider) FindSLOByTag(_ context.Context, _ string) (*model.SLO, error) {
	return f.foundByTag, f.tagErr
}
func (f *fakeProvider) CreateSLO(_ context.Context, s model.SLO) (*model.SLO, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.created != nil {
		return f.created, nil
	}
	created := s
	created.ID = "dd-slo-abc"
	return &created, nil
}
func (f *fakeProvider) UpdateSLO(_ context.Context, _ string, _ model.SLO) error {
	return f.updateErr
}

type fakeCatalog struct {
	def       *model.ServiceDefinition
	framework *model.SLOAppFramework
	defErr    error
}

func (f *fakeCatalog) GetServiceDefinition(_ context.Context, _ string) (*model.ServiceDefinition, error) {
	return f.def, f.defErr
}
func (f *fakeCatalog) DetectFramework(_ context.Context, _ string) (*model.SLOAppFramework, error) {
	return f.framework, nil
}

// --- helpers ---

func defaultSettings(requireDD bool) *config.Settings {
	return &config.Settings{
		AutoSLORequireDatadogSvc: requireDD,
		AutoSLODefaultTarget:     99.0,
		AutoSLODefaultTimeframe:  "30d",
	}
}

func minimalSLOConfig(uid, service string) *v1alpha1.SLOConfig {
	return &v1alpha1.SLOConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service + "-slo",
			Namespace: "default",
			UID:       types.UID(uid),
		},
		Spec: v1alpha1.SLOConfigSpec{
			Service:   service,
			Target:    99.0,
			Timeframe: "30d",
		},
	}
}

// --- tests ---

func TestReconcile_PathC_CreateNew(t *testing.T) {
	provider := &fakeProvider{}
	catalog := &fakeCatalog{def: &model.ServiceDefinition{DDService: "my-svc"}}
	svc := slo.NewService(provider, catalog, defaultSettings(true))

	cfg := minimalSLOConfig("uid-001", "my-svc")
	status, err := svc.Reconcile(context.Background(), cfg)

	require.NoError(t, err)
	assert.Equal(t, "synced", status.State)
	assert.NotNil(t, status.SLOID)
	assert.Equal(t, "dd-slo-abc", *status.SLOID)
}

func TestReconcile_PathC_ServiceNotInCatalog(t *testing.T) {
	provider := &fakeProvider{}
	catalog := &fakeCatalog{def: nil}
	svc := slo.NewService(provider, catalog, defaultSettings(true))

	cfg := minimalSLOConfig("uid-002", "unknown-svc")
	status, err := svc.Reconcile(context.Background(), cfg)

	require.NoError(t, err)
	assert.Equal(t, "error", status.State)
	require.NotNil(t, status.Error)
	assert.Contains(t, *status.Error, "unknown-svc")
}

func TestReconcile_PathA_FastPath_NoUpdate(t *testing.T) {
	existing := &model.SLO{
		ID:              "dd-slo-existing",
		TargetThreshold: 99.0,
		Tags:            []string{"titlis_resource_uid:uid-003"},
	}
	provider := &fakeProvider{foundByTag: existing}
	svc := slo.NewService(provider, &fakeCatalog{}, defaultSettings(false))

	cfg := minimalSLOConfig("uid-003", "my-svc")
	cfg.Status.SLOID = strPtr("dd-slo-existing")
	cfg.Spec.Target = 99.0

	status, err := svc.Reconcile(context.Background(), cfg)

	require.NoError(t, err)
	assert.Equal(t, "synced", status.State)
	assert.Equal(t, "dd-slo-existing", *status.SLOID)
}

func TestReconcile_PathA_FastPath_TargetChanged_Updates(t *testing.T) {
	existing := &model.SLO{
		ID:              "dd-slo-existing",
		TargetThreshold: 95.0,
		Tags:            []string{"titlis_resource_uid:uid-004"},
	}
	provider := &fakeProvider{foundByTag: existing}
	svc := slo.NewService(provider, &fakeCatalog{}, defaultSettings(false))

	cfg := minimalSLOConfig("uid-004", "my-svc")
	cfg.Status.SLOID = strPtr("dd-slo-existing")
	cfg.Spec.Target = 99.5

	status, err := svc.Reconcile(context.Background(), cfg)

	require.NoError(t, err)
	assert.Equal(t, "synced", status.State)
	assert.Equal(t, "dd-slo-existing", *status.SLOID)
}

func TestReconcile_PathB_OrphanSafety(t *testing.T) {
	// status.SLOID is nil, but FindSLOByTag finds the SLO (orphan recovery)
	existing := &model.SLO{
		ID:              "dd-slo-orphan",
		TargetThreshold: 99.0,
		Tags:            []string{"titlis_resource_uid:uid-005"},
	}
	provider := &fakeProvider{foundByTag: existing}
	svc := slo.NewService(provider, &fakeCatalog{}, defaultSettings(false))

	cfg := minimalSLOConfig("uid-005", "my-svc")
	// no SLOID in status — simulates orphan case

	status, err := svc.Reconcile(context.Background(), cfg)

	require.NoError(t, err)
	assert.Equal(t, "synced", status.State)
	assert.Equal(t, "dd-slo-orphan", *status.SLOID)
}

func TestReconcile_PathC_RequireDD_Disabled(t *testing.T) {
	// AutoSLORequireDatadogSvc=false → skips catalog check
	provider := &fakeProvider{}
	catalog := &fakeCatalog{def: nil}
	svc := slo.NewService(provider, catalog, defaultSettings(false))

	cfg := minimalSLOConfig("uid-006", "unlisted-svc")
	status, err := svc.Reconcile(context.Background(), cfg)

	require.NoError(t, err)
	assert.Equal(t, "synced", status.State)
	assert.NotNil(t, status.SLOID)
}

func TestReconcile_PathC_CreateError(t *testing.T) {
	provider := &fakeProvider{createErr: errors.New("datadog unavailable")}
	catalog := &fakeCatalog{def: &model.ServiceDefinition{DDService: "svc"}}
	svc := slo.NewService(provider, catalog, defaultSettings(true))

	cfg := minimalSLOConfig("uid-007", "svc")
	_, err := svc.Reconcile(context.Background(), cfg)

	assert.ErrorContains(t, err, "datadog unavailable")
}

func TestReconcile_FrameworkFromAnnotation(t *testing.T) {
	provider := &fakeProvider{}
	catalog := &fakeCatalog{def: &model.ServiceDefinition{DDService: "svc"}}
	svc := slo.NewService(provider, catalog, defaultSettings(true))

	cfg := minimalSLOConfig("uid-008", "svc")
	cfg.Annotations = map[string]string{"titlis.io/app-framework": "fastapi"}

	status, err := svc.Reconcile(context.Background(), cfg)

	require.NoError(t, err)
	assert.Equal(t, "synced", status.State)
	// framework detected from annotation; SLO name should contain "fastapi"
	assert.NotNil(t, status.SLOID)
}

func strPtr(s string) *string { return &s }
