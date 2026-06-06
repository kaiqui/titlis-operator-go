package scorecard_test

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
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
