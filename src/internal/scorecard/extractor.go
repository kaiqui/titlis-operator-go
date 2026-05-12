package scorecard

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LookupHPA returns the HPA that targets deploy, or nil if none exists.
func LookupHPA(ctx context.Context, ns, deployName string, k8s client.Client) *autoscalingv2.HorizontalPodAutoscaler {
	return findHPA(ctx, ns, deployName, k8s)
}

// HasNetworkPolicy returns true if any NetworkPolicy exists in the namespace.
func HasNetworkPolicy(ctx context.Context, ns string, k8s client.Client) bool {
	return networkPolicyExists(ctx, ns, k8s)
}

func findHPA(ctx context.Context, ns, deployName string, k8s client.Client) *autoscalingv2.HorizontalPodAutoscaler {
	var list autoscalingv2.HorizontalPodAutoscalerList
	if err := k8s.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil
	}
	for i, h := range list.Items {
		ref := h.Spec.ScaleTargetRef
		if ref.Kind == "Deployment" && ref.Name == deployName {
			return &list.Items[i]
		}
	}
	return nil
}

func networkPolicyExists(ctx context.Context, ns string, k8s client.Client) bool {
	var list networkingv1.NetworkPolicyList
	if err := k8s.List(ctx, &list, client.InNamespace(ns), &client.ListOptions{
		LabelSelector: labels.Everything(),
	}); err != nil {
		return false
	}
	return len(list.Items) > 0
}

// ExtractSnapshot builds a WorkloadSnapshot from a Deployment and pre-fetched HPA / network policy state.
func ExtractSnapshot(
	deploy *appsv1.Deployment,
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	hasNetPolicy bool,
	cluster, engineSlug string,
) WorkloadSnapshot {
	snap := WorkloadSnapshot{
		UID:              string(deploy.UID),
		Name:             deploy.Name,
		Namespace:        deploy.Namespace,
		Cluster:          cluster,
		Kind:             "Deployment",
		Labels:           deploy.Labels,
		EngineSlug:       engineSlug,
		HasNetworkPolicy: hasNetPolicy,
		HasPodSecurityContext: deploy.Spec.Template.Spec.SecurityContext != nil,
	}

	if deploy.Annotations["titlis.io/criticality"] == "high" {
		snap.Criticality = "high"
	} else {
		snap.Criticality = "standard"
	}

	if deploy.Spec.Replicas != nil {
		snap.Replicas = *deploy.Spec.Replicas
	} else {
		snap.Replicas = 1
	}
	snap.Strategy = string(deploy.Spec.Strategy.Type)
	if deploy.Spec.Template.Spec.TerminationGracePeriodSeconds != nil {
		snap.TerminationGracePeriodSec = *deploy.Spec.Template.Spec.TerminationGracePeriodSeconds
	}

	if len(deploy.Spec.Template.Spec.Containers) > 0 {
		c := deploy.Spec.Template.Spec.Containers[0]
		snap.HasLivenessProbe = c.LivenessProbe != nil
		snap.HasReadinessProbe = c.ReadinessProbe != nil
		snap.CPURequestSet = !c.Resources.Requests.Cpu().IsZero()
		snap.CPULimitSet = !c.Resources.Limits.Cpu().IsZero()
		snap.MemoryRequestSet = !c.Resources.Requests.Memory().IsZero()
		snap.MemoryLimitSet = !c.Resources.Limits.Memory().IsZero()
		if snap.CPURequestSet && snap.CPULimitSet {
			req := c.Resources.Requests.Cpu().AsApproximateFloat64()
			if req > 0 {
				snap.CPULimitRatio = c.Resources.Limits.Cpu().AsApproximateFloat64() / req
			}
		}
		snap.ImageTag = c.Image

		csc := c.SecurityContext
		if csc != nil {
			if csc.ReadOnlyRootFilesystem != nil {
				snap.ReadOnlyRootFS = *csc.ReadOnlyRootFilesystem
			}
			if csc.RunAsNonRoot != nil {
				snap.RunAsNonRoot = *csc.RunAsNonRoot
			}
			// AllowPrivilegeEscalation=true in snapshot means escalation is NOT blocked
			snap.AllowPrivilegeEscalation = csc.AllowPrivilegeEscalation == nil || *csc.AllowPrivilegeEscalation
			if csc.Capabilities != nil {
				snap.HasDropCapabilities = len(csc.Capabilities.Drop) > 0
			}
		} else {
			snap.AllowPrivilegeEscalation = true
		}
	}

	snap.HPAScaleUpStabilizationSec = -1
	snap.HPAScaleDownStabilizationSec = -1

	if hpa != nil {
		snap.HasHPA = true
		snap.HPAHasMetrics = len(hpa.Spec.Metrics) > 0
		if hpa.Spec.MinReplicas != nil {
			snap.HPAMinReplicas = int(*hpa.Spec.MinReplicas)
		} else {
			snap.HPAMinReplicas = 1
		}
		for _, m := range hpa.Spec.Metrics {
			if m.Type == autoscalingv2.ResourceMetricSourceType &&
				m.Resource != nil &&
				m.Resource.Name == "cpu" &&
				m.Resource.Target.AverageUtilization != nil {
				snap.HPACPUTargetPercent = int(*m.Resource.Target.AverageUtilization)
				break
			}
		}
		if hpa.Spec.Behavior != nil {
			if hpa.Spec.Behavior.ScaleUp != nil &&
				hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds != nil {
				snap.HPAScaleUpStabilizationSec = int(*hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds)
			}
			if hpa.Spec.Behavior.ScaleDown != nil &&
				hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
				snap.HPAScaleDownStabilizationSec = int(*hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds)
			}
			hasUp := hpa.Spec.Behavior.ScaleUp != nil && len(hpa.Spec.Behavior.ScaleUp.Policies) > 0
			hasDown := hpa.Spec.Behavior.ScaleDown != nil && len(hpa.Spec.Behavior.ScaleDown.Policies) > 0
			snap.HPAHasBehaviorPolicies = hasUp && hasDown
		}
	}

	return snap
}
