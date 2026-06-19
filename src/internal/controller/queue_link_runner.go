package controller

import (
	"context"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/titlis/operator/internal/queue"
)

// QueueLinkAPIClient abstracts the titlisapi.Client methods used by QueueLinkRunner.
type QueueLinkAPIClient interface {
	GetQueueNames(ctx context.Context) ([]queue.QueueName, error)
	SendQueueLinkHints(ctx context.Context, hints []queue.QueueLinkHint)
}

// QueueLinkRunner correlates queues to workloads by matching known queue names against Deployment
// env var values (inline + referenced ConfigMaps) in-cluster. It never reads Secrets and sends
// only the matches it finds. This is the cheap, architecture-aligned alternative to scanning
// GitHub repos: no fleet sweep, no APM, no raw env leaves the cluster.
type QueueLinkRunner struct {
	TitlisAPI QueueLinkAPIClient
	K8s       client.Client
	Interval  time.Duration
	Log       logr.Logger
}

func (r *QueueLinkRunner) Start(ctx context.Context) error {
	r.run(ctx)
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.run(ctx)
		}
	}
}

// RunOnce executes a single scan. Used in tests.
func (r *QueueLinkRunner) RunOnce(ctx context.Context) { r.run(ctx) }

func (r *QueueLinkRunner) run(ctx context.Context) {
	logger := r.Log

	names, err := r.TitlisAPI.GetQueueNames(ctx)
	if err != nil {
		logger.Error(err, "queuelink: failed to get queue names")
		return
	}
	if len(names) == 0 {
		return
	}

	var deploys appsv1.DeploymentList
	if err := r.K8s.List(ctx, &deploys); err != nil {
		logger.Error(err, "queuelink: failed to list deployments")
		return
	}

	cmCache := map[string]*corev1.ConfigMap{}
	type hintKey struct{ externalID, workloadUID string }
	seen := map[hintKey]bool{}
	var hints []queue.QueueLinkHint

	for i := range deploys.Items {
		d := &deploys.Items[i]
		candidates := r.collectEnvValues(ctx, d, cmCache)
		if len(candidates) == 0 {
			continue
		}
		for _, qn := range names {
			if !matchesAny(candidates, qn) {
				continue
			}
			k := hintKey{externalID: qn.ExternalID, workloadUID: string(d.UID)}
			if seen[k] {
				continue
			}
			seen[k] = true
			hints = append(hints, queue.QueueLinkHint{
				ExternalID:   qn.ExternalID,
				DisplayName:  qn.DisplayName,
				WorkloadUID:  string(d.UID),
				WorkloadName: d.Name,
				Namespace:    d.Namespace,
			})
		}
	}

	if len(hints) == 0 {
		return
	}
	logger.Info("queuelink: sending hints", "count", len(hints))
	r.TitlisAPI.SendQueueLinkHints(ctx, hints)
}

// collectEnvValues gathers inline env values and referenced ConfigMap values. Secrets are skipped.
func (r *QueueLinkRunner) collectEnvValues(ctx context.Context, d *appsv1.Deployment, cmCache map[string]*corev1.ConfigMap) []string {
	var out []string
	containers := append([]corev1.Container{}, d.Spec.Template.Spec.Containers...)
	containers = append(containers, d.Spec.Template.Spec.InitContainers...)

	for _, c := range containers {
		for _, e := range c.Env {
			if e.Value != "" {
				out = append(out, e.Value)
				continue
			}
			if e.ValueFrom != nil && e.ValueFrom.ConfigMapKeyRef != nil {
				ref := e.ValueFrom.ConfigMapKeyRef
				if cm := r.getConfigMap(ctx, d.Namespace, ref.Name, cmCache); cm != nil {
					if v, ok := cm.Data[ref.Key]; ok {
						out = append(out, v)
					}
				}
			}
		}
		for _, ef := range c.EnvFrom {
			if ef.ConfigMapRef == nil {
				continue
			}
			if cm := r.getConfigMap(ctx, d.Namespace, ef.ConfigMapRef.Name, cmCache); cm != nil {
				for _, v := range cm.Data {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

func (r *QueueLinkRunner) getConfigMap(ctx context.Context, ns, name string, cache map[string]*corev1.ConfigMap) *corev1.ConfigMap {
	key := ns + "/" + name
	if cm, ok := cache[key]; ok {
		return cm
	}
	var cm corev1.ConfigMap
	if err := r.K8s.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &cm); err != nil {
		cache[key] = nil
		return nil
	}
	cache[key] = &cm
	return &cm
}

// matchesAny returns true if any candidate string references the queue (by external id, display
// name, or display name as a substring when distinctive enough).
func matchesAny(candidates []string, qn queue.QueueName) bool {
	for _, c := range candidates {
		if c == qn.ExternalID || c == qn.DisplayName {
			return true
		}
		if qn.ExternalID != "" && strings.Contains(c, qn.ExternalID) {
			return true
		}
		if len(qn.DisplayName) >= 4 && strings.Contains(c, qn.DisplayName) {
			return true
		}
	}
	return false
}
