package castai

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// monitoredServices mirrors the Python operator's CASTAI_SERVICES list.
var monitoredServices = []string{
	"castai-agent",
	"castai-cluster-controller",
}

// PodHealthResult holds the health status of a single CAST AI service pod.
type PodHealthResult struct {
	Service     string
	Namespace   string
	ClusterName string
	IsHealthy   bool
	PodName     string
	Reason      string
}

// HealthChecker checks the health of CAST AI pods via the K8s API.
type HealthChecker struct {
	k8s         client.Client
	namespace   string
	clusterName string
}

func NewHealthChecker(k8s client.Client, namespace, clusterName string) *HealthChecker {
	return &HealthChecker{k8s: k8s, namespace: namespace, clusterName: clusterName}
}

func (h *HealthChecker) CheckAll(ctx context.Context) []PodHealthResult {
	results := make([]PodHealthResult, 0, len(monitoredServices))
	for _, svc := range monitoredServices {
		results = append(results, h.checkService(ctx, svc))
	}
	return results
}

func (h *HealthChecker) checkService(ctx context.Context, service string) PodHealthResult {
	base := PodHealthResult{
		Service:     service,
		Namespace:   h.namespace,
		ClusterName: h.clusterName,
	}

	// Try both label conventions used by CAST AI.
	pods, err := h.findPods(ctx, service)
	if err != nil {
		base.Reason = fmt.Sprintf("erro ao listar pods: %v", err)
		return base
	}
	if len(pods) == 0 {
		base.Reason = "nenhum pod encontrado"
		return base
	}

	// Pick the most recently created pod.
	newest := pods[0]
	for _, p := range pods[1:] {
		if p.CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = p
		}
	}

	base.PodName = newest.Name
	base.IsHealthy, base.Reason = evaluatePod(newest)
	return base
}

func (h *HealthChecker) findPods(ctx context.Context, service string) ([]corev1.Pod, error) {
	for _, selector := range []map[string]string{
		{"app": service},
		{"app.kubernetes.io/name": service},
	} {
		var list corev1.PodList
		if err := h.k8s.List(ctx, &list,
			client.InNamespace(h.namespace),
			client.MatchingLabels(selector),
		); err != nil {
			return nil, err
		}
		if len(list.Items) > 0 {
			return list.Items, nil
		}
	}
	return nil, nil
}

func evaluatePod(pod corev1.Pod) (healthy bool, reason string) {
	if pod.Status.Phase != corev1.PodRunning {
		phase := string(pod.Status.Phase)
		if phase == "" {
			phase = "desconhecida"
		}
		return false, "phase atual: " + phase
	}

	for _, c := range pod.Status.Conditions {
		if c.Type != corev1.PodReady {
			continue
		}
		if c.Status != corev1.ConditionTrue {
			r := c.Reason
			if r == "" {
				r = "não-Ready"
			}
			return false, "pod não está Ready: " + r
		}
		return true, "Running e Ready"
	}

	return false, "condição Ready ausente"
}
