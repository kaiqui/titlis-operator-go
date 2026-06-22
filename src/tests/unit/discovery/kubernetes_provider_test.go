package discovery_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/titlis/operator/internal/discovery"
	k8sdiscovery "github.com/titlis/operator/internal/discovery/kubernetes"
)

type stubExcluder struct{ excluded map[string]bool }

func (s stubExcluder) IsExcluded(ns string) bool { return s.excluded[ns] }

func hasAsset(assets []discovery.Asset, kind, name string) bool {
	for _, a := range assets {
		if a.Kind == kind && a.Name == name {
			return true
		}
	}
	return false
}

func hasRelation(rels []discovery.Relation, src, tgt, typ string) bool {
	for _, r := range rels {
		if r.SourceExternalID == src && r.TargetExternalID == tgt && r.Type == typ {
			return true
		}
	}
	return false
}

func getAsset(assets []discovery.Asset, kind, name string) *discovery.Asset {
	for i := range assets {
		if assets[i].Kind == kind && assets[i].Name == name {
			return &assets[i]
		}
	}
	return nil
}

func TestKubernetesProvider_SurfacesUSTagOnTemplateLabel(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-api", Namespace: "shop", UID: types.UID("dep-1")},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				// USTag present only on the pod template, not the workload labels.
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"tags.datadoghq.com/service": "orders"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "orders:1"}}},
			},
		},
	}
	cl := ctrlfake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(deploy).Build()

	p := k8sdiscovery.New(cl, stubExcluder{excluded: map[string]bool{}}, "prod-k8s")
	sub, err := p.Discover(context.Background())

	assert.NoError(t, err)
	a := getAsset(sub.Assets, "deployment", "orders-api")
	assert.NotNil(t, a)
	assert.Equal(t, "orders", a.Attributes["ddService"])
}

func TestKubernetesProvider_DiscoversGraphAndEdges(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "shop", UID: types.UID("ns-1")}}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-api", Namespace: "shop", UID: types.UID("dep-1")},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "orders"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "app",
						Image: "orders:1.0",
						EnvFrom: []corev1.EnvFromSource{{
							ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "orders-cfg"}},
						}},
						Env: []corev1.EnvVar{{
							Name: "DB_PASS",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "orders-sec"}, Key: "p"},
							},
						}},
					}},
				},
			},
		},
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-svc", Namespace: "shop", UID: types.UID("svc-1")},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Selector: map[string]string{"app": "orders"}},
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-hpa", Namespace: "shop", UID: types.UID("hpa-1")},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "orders-api"},
			MaxReplicas:    5,
		},
	}

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "orders-cfg", Namespace: "shop", UID: types.UID("cm-1")}}
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-sec", Namespace: "shop", UID: types.UID("sec-1")}, Type: corev1.SecretTypeOpaque}

	// A workload in an excluded namespace must not appear.
	excludedDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "kube-thing", Namespace: "kube-system", UID: types.UID("dep-x")},
	}

	cl := ctrlfake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(ns, deploy, svc, hpa, cm, sec, excludedDeploy).Build()

	p := k8sdiscovery.New(cl, stubExcluder{excluded: map[string]bool{"kube-system": true}}, "prod-k8s")
	sub, err := p.Discover(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, discovery.StatusOK, sub.Status.Status)

	// Assets of every kind present; excluded workload absent.
	assert.True(t, hasAsset(sub.Assets, "deployment", "orders-api"))
	assert.True(t, hasAsset(sub.Assets, "service", "orders-svc"))
	assert.True(t, hasAsset(sub.Assets, "hpa", "orders-hpa"))
	assert.True(t, hasAsset(sub.Assets, "configmap", "orders-cfg"))
	assert.True(t, hasAsset(sub.Assets, "secret", "orders-sec"))
	assert.True(t, hasAsset(sub.Assets, "namespace", "shop"))
	assert.False(t, hasAsset(sub.Assets, "deployment", "kube-thing"))

	// Edges.
	assert.True(t, hasRelation(sub.Relations, "svc-1", "dep-1", "selects"))
	assert.True(t, hasRelation(sub.Relations, "dep-1", "hpa-1", "scaled_by"))
	assert.True(t, hasRelation(sub.Relations, "dep-1", "cm-1", "uses_config"))
	assert.True(t, hasRelation(sub.Relations, "dep-1", "sec-1", "uses_secret"))
}
