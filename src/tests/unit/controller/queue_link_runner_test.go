package controller_test

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/titlis/operator/internal/controller"
	"github.com/titlis/operator/internal/queue"
)

type fakeLinkAPI struct {
	names []queue.QueueName
	sent  []queue.QueueLinkHint
}

func (f *fakeLinkAPI) GetQueueNames(_ context.Context) ([]queue.QueueName, error) { return f.names, nil }
func (f *fakeLinkAPI) SendQueueLinkHints(_ context.Context, hints []queue.QueueLinkHint) {
	f.sent = append(f.sent, hints...)
}

func deployWithEnv(name string, uid string, env []corev1.EnvVar) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "shop", UID: types.UID(uid)},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Env: env}},
				},
			},
		},
	}
}

func TestQueueLinkRunner_MatchesInlineEnv(t *testing.T) {
	deploy := deployWithEnv("orders-api", "uid-1", []corev1.EnvVar{
		{Name: "PUBSUB_SUBSCRIPTION", Value: "orders-events-sub"},
	})
	cl := ctrlfake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(deploy).Build()
	api := &fakeLinkAPI{names: []queue.QueueName{
		{ExternalID: "projects/p/subscriptions/orders-events-sub", DisplayName: "orders-events-sub"},
	}}

	r := &controller.QueueLinkRunner{TitlisAPI: api, K8s: cl, Log: logr.Discard()}
	r.RunOnce(context.Background())

	assert.Len(t, api.sent, 1)
	assert.Equal(t, "orders-api", api.sent[0].WorkloadName)
	assert.Equal(t, "shop", api.sent[0].Namespace)
	assert.Equal(t, "projects/p/subscriptions/orders-events-sub", api.sent[0].ExternalID)
}

func TestQueueLinkRunner_MatchesConfigMapEnvFrom(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "billing", Namespace: "shop", UID: "uid-3"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "app",
				EnvFrom: []corev1.EnvFromSource{{
					ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "billing-cfg"}},
				}},
			}},
		}}},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "billing-cfg", Namespace: "shop"},
		Data:       map[string]string{"SUB": "billing-sub"},
	}
	cl := ctrlfake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(deploy, cm).Build()
	api := &fakeLinkAPI{names: []queue.QueueName{{ExternalID: "x/billing-sub", DisplayName: "billing-sub"}}}

	r := &controller.QueueLinkRunner{TitlisAPI: api, K8s: cl, Log: logr.Discard()}
	r.RunOnce(context.Background())

	assert.Len(t, api.sent, 1)
	assert.Equal(t, "billing", api.sent[0].WorkloadName)
}

func TestQueueLinkRunner_NoMatch(t *testing.T) {
	deploy := deployWithEnv("other", "uid-2", []corev1.EnvVar{{Name: "FOO", Value: "bar"}})
	cl := ctrlfake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(deploy).Build()
	api := &fakeLinkAPI{names: []queue.QueueName{{ExternalID: "projects/p/subscriptions/orders-events-sub", DisplayName: "orders-events-sub"}}}

	r := &controller.QueueLinkRunner{TitlisAPI: api, K8s: cl, Log: logr.Discard()}
	r.RunOnce(context.Background())

	assert.Empty(t, api.sent)
}
