package scorecard_test

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/titlis/operator/internal/scorecard"
)

func minimalDeploy() *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "uid-test",
			Name:      "my-svc",
			Namespace: "production",
			Labels:    map[string]string{},
			Annotations: map[string]string{},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "my-svc:v1.0.0",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestExtractSnapshot_BackstageComponent_KubernetesID(t *testing.T) {
	deploy := minimalDeploy()
	deploy.Annotations["backstage.io/kubernetes-id"] = "my-service"

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")
	if snap.BackstageComponent != "my-service" {
		t.Errorf("esperava BackstageComponent='my-service', got='%s'", snap.BackstageComponent)
	}
}

func TestExtractSnapshot_BackstageComponent_EntityNameFallback(t *testing.T) {
	deploy := minimalDeploy()
	deploy.Annotations["backstage.io/entity-name"] = "my-service-entity"

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")
	if snap.BackstageComponent != "my-service-entity" {
		t.Errorf("esperava BackstageComponent='my-service-entity', got='%s'", snap.BackstageComponent)
	}
}

func TestExtractSnapshot_BackstageComponent_KubernetesIDTakesPriority(t *testing.T) {
	deploy := minimalDeploy()
	deploy.Annotations["backstage.io/kubernetes-id"] = "primary-name"
	deploy.Annotations["backstage.io/entity-name"] = "secondary-name"

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")
	if snap.BackstageComponent != "primary-name" {
		t.Errorf("esperava 'primary-name' (kubernetes-id tem prioridade), got='%s'", snap.BackstageComponent)
	}
}

func TestExtractSnapshot_BackstageComponent_Empty_WhenNoAnnotation(t *testing.T) {
	deploy := minimalDeploy()

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")
	if snap.BackstageComponent != "" {
		t.Errorf("esperava BackstageComponent vazio, got='%s'", snap.BackstageComponent)
	}
}

func TestExtractSnapshot_BackstageComponent_Empty_WhenAnnotationsNil(t *testing.T) {
	deploy := minimalDeploy()
	deploy.Annotations = nil

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")
	if snap.BackstageComponent != "" {
		t.Errorf("esperava BackstageComponent vazio com annotations nil, got='%s'", snap.BackstageComponent)
	}
}

func TestExtractSnapshot_BasicFields(t *testing.T) {
	deploy := minimalDeploy()
	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")

	if snap.UID != "uid-test" {
		t.Errorf("UID incorreto: %s", snap.UID)
	}
	if snap.Name != "my-svc" {
		t.Errorf("Name incorreto: %s", snap.Name)
	}
	if snap.Cluster != "prod-cluster" {
		t.Errorf("Cluster incorreto: %s", snap.Cluster)
	}
	if snap.EngineSlug != "kubernetes" {
		t.Errorf("EngineSlug incorreto: %s", snap.EngineSlug)
	}
	if snap.Criticality != "standard" {
		t.Errorf("Criticality esperava 'standard', got: %s", snap.Criticality)
	}
}

func TestExtractSnapshot_Criticality_High(t *testing.T) {
	deploy := minimalDeploy()
	deploy.Annotations["titlis.io/criticality"] = "high"

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")
	if snap.Criticality != "high" {
		t.Errorf("esperava criticality='high', got='%s'", snap.Criticality)
	}
}

func TestExtractSnapshot_HasDatadog_WhenLabelPresent(t *testing.T) {
	deploy := minimalDeploy()
	deploy.Labels["tags.datadoghq.com/service"] = "my-svc"

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")
	if !snap.HasDatadog {
		t.Error("esperava HasDatadog=true quando label de service está presente")
	}
}

func TestExtractSnapshot_ServiceRepo_FromAnnotation(t *testing.T) {
	deploy := minimalDeploy()
	deploy.Annotations["titlis.io/service-repo"] = "github.com/myorg/myrepo"

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")
	if snap.ServiceRepo != "github.com/myorg/myrepo" {
		t.Errorf("esperava ServiceRepo='github.com/myorg/myrepo', got='%s'", snap.ServiceRepo)
	}
}

func TestExtractSnapshot_ServiceRepo_EmptyWhenAnnotationAbsent(t *testing.T) {
	deploy := minimalDeploy()

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")
	if snap.ServiceRepo != "" {
		t.Errorf("esperava ServiceRepo vazio, got='%s'", snap.ServiceRepo)
	}
}

func TestExtractSnapshot_WithHPA_SetsHPAFields(t *testing.T) {
	deploy := minimalDeploy()
	minReplicas := int32(2)
	cpuTarget := int32(70)
	scaleUpSec := int32(60)
	scaleDownSec := int32(120)

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: "cpu",
						Target: autoscalingv2.MetricTarget{
							AverageUtilization: &cpuTarget,
						},
					},
				},
			},
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &scaleUpSec,
					Policies:                   []autoscalingv2.HPAScalingPolicy{{Type: autoscalingv2.PodsScalingPolicy, Value: 4, PeriodSeconds: 60}},
				},
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &scaleDownSec,
					Policies:                   []autoscalingv2.HPAScalingPolicy{{Type: autoscalingv2.PodsScalingPolicy, Value: 1, PeriodSeconds: 60}},
				},
			},
		},
	}

	snap := scorecard.ExtractSnapshot(deploy, hpa, false, "prod-cluster", "kubernetes")

	if !snap.HasHPA {
		t.Error("esperava HasHPA=true")
	}
	if !snap.HPAHasMetrics {
		t.Error("esperava HPAHasMetrics=true")
	}
	if snap.HPAMinReplicas != 2 {
		t.Errorf("esperava HPAMinReplicas=2, got=%d", snap.HPAMinReplicas)
	}
	if snap.HPACPUTargetPercent != 70 {
		t.Errorf("esperava HPACPUTargetPercent=70, got=%d", snap.HPACPUTargetPercent)
	}
	if snap.HPAScaleUpStabilizationSec != 60 {
		t.Errorf("esperava HPAScaleUpStabilizationSec=60, got=%d", snap.HPAScaleUpStabilizationSec)
	}
	if snap.HPAScaleDownStabilizationSec != 120 {
		t.Errorf("esperava HPAScaleDownStabilizationSec=120, got=%d", snap.HPAScaleDownStabilizationSec)
	}
	if !snap.HPAHasBehaviorPolicies {
		t.Error("esperava HPAHasBehaviorPolicies=true")
	}
}

func TestExtractSnapshot_WithHPA_NoMinReplicas_DefaultsToOne(t *testing.T) {
	deploy := minimalDeploy()
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{},
	}

	snap := scorecard.ExtractSnapshot(deploy, hpa, false, "prod-cluster", "kubernetes")

	if snap.HPAMinReplicas != 1 {
		t.Errorf("esperava HPAMinReplicas=1 quando nil, got=%d", snap.HPAMinReplicas)
	}
}

func TestExtractSnapshot_SecurityContext_ReadOnlyRootFS(t *testing.T) {
	deploy := minimalDeploy()
	readOnly := true
	runAsNonRoot := true
	allowEscalation := false
	deploy.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
		ReadOnlyRootFilesystem:   &readOnly,
		RunAsNonRoot:             &runAsNonRoot,
		AllowPrivilegeEscalation: &allowEscalation,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")

	if !snap.ReadOnlyRootFS {
		t.Error("esperava ReadOnlyRootFS=true")
	}
	if !snap.RunAsNonRoot {
		t.Error("esperava RunAsNonRoot=true")
	}
	if snap.AllowPrivilegeEscalation {
		t.Error("esperava AllowPrivilegeEscalation=false (escalação bloqueada)")
	}
	if !snap.HasDropCapabilities {
		t.Error("esperava HasDropCapabilities=true")
	}
}

func TestExtractSnapshot_NoSecurityContext_AllowsEscalation(t *testing.T) {
	deploy := minimalDeploy()
	deploy.Spec.Template.Spec.Containers[0].SecurityContext = nil

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")

	if !snap.AllowPrivilegeEscalation {
		t.Error("esperava AllowPrivilegeEscalation=true quando SecurityContext é nil")
	}
}

func TestExtractSnapshot_HasNetworkPolicy_PropagatesFlag(t *testing.T) {
	deploy := minimalDeploy()

	snap := scorecard.ExtractSnapshot(deploy, nil, true, "prod-cluster", "kubernetes")

	if !snap.HasNetworkPolicy {
		t.Error("esperava HasNetworkPolicy=true")
	}
}

func TestExtractSnapshot_ReplicasNilDefaultsToOne(t *testing.T) {
	deploy := minimalDeploy()
	deploy.Spec.Replicas = nil

	snap := scorecard.ExtractSnapshot(deploy, nil, false, "prod-cluster", "kubernetes")

	if snap.Replicas != 1 {
		t.Errorf("esperava Replicas=1 quando nil, got=%d", snap.Replicas)
	}
}
