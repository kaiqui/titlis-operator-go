//go:build integration

package integration_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/titlis/operator/api/v1alpha1"
)

func minimalDeployment(name, ns string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:1.25",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
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

var _ = Describe("ScorecardController", func() {
	const timeout = 15 * time.Second
	const interval = 250 * time.Millisecond

	Context("when a Deployment is created", func() {
		It("creates an AppScorecard with a score", func() {
			deploy := minimalDeployment("test-deploy", "default")
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, deploy)
			})

			key := types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}
			asc := &v1alpha1.AppScorecard{}

			Eventually(func() bool {
				if err := k8sClient.Get(ctx, key, asc); err != nil {
					return false
				}
				return asc.Status.OverallScore != nil
			}, timeout, interval).Should(BeTrue(), "AppScorecard should have a score")

			Expect(*asc.Status.OverallScore).To(BeNumerically(">=", 0))
			Expect(*asc.Status.OverallScore).To(BeNumerically("<=", 100))
			Expect(asc.Status.ComplianceStatus).NotTo(BeEmpty())
		})

		It("sets NON_COMPLIANT status for a bare deployment", func() {
			deploy := minimalDeployment("bare-deploy", "default")
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, deploy)
			})

			key := types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}
			asc := &v1alpha1.AppScorecard{}

			Eventually(func() string {
				if err := k8sClient.Get(ctx, key, asc); err != nil {
					return ""
				}
				return asc.Status.ComplianceStatus
			}, timeout, interval).Should(Equal("non_compliant"))
		})

		It("score does not decrease on re-reconcile", func() {
			deploy := minimalDeployment("stable-deploy", "default")
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, deploy)
			})

			key := types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}
			asc := &v1alpha1.AppScorecard{}

			Eventually(func() bool {
				if err := k8sClient.Get(ctx, key, asc); err != nil {
					return false
				}
				return asc.Status.OverallScore != nil
			}, timeout, interval).Should(BeTrue())

			firstScore := *asc.Status.OverallScore

			// Touch the deployment to trigger a second reconcile
			patch := k8sClient.MergeFrom(deploy.DeepCopy())
			if deploy.Annotations == nil {
				deploy.Annotations = map[string]string{}
			}
			deploy.Annotations["titlis.io/test-touch"] = "1"
			Expect(k8sClient.Patch(ctx, deploy, patch)).To(Succeed())

			// Wait for re-reconcile and confirm score did not decrease
			Consistently(func() int32 {
				if err := k8sClient.Get(ctx, key, asc); err != nil {
					return firstScore
				}
				if asc.Status.OverallScore == nil {
					return firstScore
				}
				return *asc.Status.OverallScore
			}, 5*time.Second, interval).Should(BeNumerically(">=", firstScore))
		})
	})
})
