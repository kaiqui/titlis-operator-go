package kubernetes

import (
	"context"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/titlis/operator/internal/discovery"
)

const providerName = "kubernetes"

// excluder is the subset of scorecard.ExclusionFilter the provider needs.
type excluder interface {
	IsExcluded(ns string) bool
}

// Provider is the native discovery source. It enumerates the cluster topology — not just
// Deployments — into a normalized asset graph. It reads via an uncached client.Reader so a sweep
// is a handful of List calls every interval instead of cluster-wide informers (avoids caching all
// Secrets/ConfigMaps).
type Provider struct {
	k8s     client.Reader
	excl    excluder
	cluster string
}

func New(k8s client.Reader, excl excluder, cluster string) *Provider {
	return &Provider{k8s: k8s, excl: excl, cluster: cluster}
}

func (p *Provider) Name() string  { return providerName }
func (p *Provider) Enabled() bool { return true }

func (p *Provider) Discover(ctx context.Context) (discovery.AssetSubgraph, error) {
	b := newBuilder(p.cluster)

	// Index-building resources first so workload edges can resolve names → UIDs.
	p.namespaces(ctx, b)
	p.configMaps(ctx, b)
	p.secrets(ctx, b)
	p.services(ctx, b)
	// Workloads + the rest.
	p.deployments(ctx, b)
	p.statefulSets(ctx, b)
	p.daemonSets(ctx, b)
	p.cronJobs(ctx, b)
	p.ingresses(ctx, b)
	p.hpas(ctx, b)
	p.pdbs(ctx, b)
	p.networkPolicies(ctx, b)

	b.resolveEdges()

	status := discovery.ProviderStatus{Status: discovery.StatusOK}
	if len(b.listErrors) > 0 {
		status.Status = discovery.StatusPartial
		status.Error = strings.Join(b.listErrors, "; ")
	}
	return discovery.AssetSubgraph{Assets: b.assets, Relations: b.relations, Status: status}, nil
}

// --- resource listing ---

func (p *Provider) namespaces(ctx context.Context, b *builder) {
	var list corev1.NamespaceList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("namespaces", err)
		return
	}
	for i := range list.Items {
		ns := &list.Items[i]
		if p.excl.IsExcluded(ns.Name) {
			continue
		}
		b.add(discovery.Asset{
			ExternalID: string(ns.UID), Provider: providerName, Kind: "namespace",
			Name: ns.Name, Cluster: p.cluster, Tags: ns.Labels,
			Attributes: map[string]any{"phase": string(ns.Status.Phase)},
		})
	}
}

func (p *Provider) configMaps(ctx context.Context, b *builder) {
	var list corev1.ConfigMapList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("configmaps", err)
		return
	}
	for i := range list.Items {
		cm := &list.Items[i]
		if p.excl.IsExcluded(cm.Namespace) {
			continue
		}
		b.configMapUID[nn(cm.Namespace, cm.Name)] = string(cm.UID)
		b.add(discovery.Asset{
			ExternalID: string(cm.UID), Provider: providerName, Kind: "configmap",
			Name: cm.Name, Namespace: cm.Namespace, Cluster: p.cluster, Tags: cm.Labels,
			Attributes: map[string]any{"keys": len(cm.Data)},
		})
	}
}

func (p *Provider) secrets(ctx context.Context, b *builder) {
	var list corev1.SecretList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("secrets", err)
		return
	}
	for i := range list.Items {
		s := &list.Items[i]
		if p.excl.IsExcluded(s.Namespace) {
			continue
		}
		b.secretUID[nn(s.Namespace, s.Name)] = string(s.UID)
		// Metadata only — never the secret data.
		b.add(discovery.Asset{
			ExternalID: string(s.UID), Provider: providerName, Kind: "secret",
			Name: s.Name, Namespace: s.Namespace, Cluster: p.cluster, Tags: s.Labels,
			Attributes: map[string]any{"type": string(s.Type)},
		})
	}
}

func (p *Provider) services(ctx context.Context, b *builder) {
	var list corev1.ServiceList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("services", err)
		return
	}
	for i := range list.Items {
		svc := &list.Items[i]
		if p.excl.IsExcluded(svc.Namespace) {
			continue
		}
		uid := string(svc.UID)
		b.serviceUID[nn(svc.Namespace, svc.Name)] = uid
		b.add(discovery.Asset{
			ExternalID: uid, Provider: providerName, Kind: "service",
			Name: svc.Name, Namespace: svc.Namespace, Cluster: p.cluster, Tags: svc.Labels,
			Attributes: map[string]any{"type": string(svc.Spec.Type), "ports": len(svc.Spec.Ports)},
		})
		b.services = append(b.services, svcRef{uid: uid, namespace: svc.Namespace, selector: svc.Spec.Selector})
	}
}

func (p *Provider) deployments(ctx context.Context, b *builder) {
	var list appsv1.DeploymentList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("deployments", err)
		return
	}
	for i := range list.Items {
		d := &list.Items[i]
		if p.excl.IsExcluded(d.Namespace) {
			continue
		}
		b.addWorkload(string(d.UID), "Deployment", d.Namespace, d.Name, d.Labels,
			d.Spec.Template.Labels, d.Spec.Template.Spec, d.Spec.Replicas)
	}
}

func (p *Provider) statefulSets(ctx context.Context, b *builder) {
	var list appsv1.StatefulSetList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("statefulsets", err)
		return
	}
	for i := range list.Items {
		s := &list.Items[i]
		if p.excl.IsExcluded(s.Namespace) {
			continue
		}
		b.addWorkload(string(s.UID), "StatefulSet", s.Namespace, s.Name, s.Labels,
			s.Spec.Template.Labels, s.Spec.Template.Spec, s.Spec.Replicas)
	}
}

func (p *Provider) daemonSets(ctx context.Context, b *builder) {
	var list appsv1.DaemonSetList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("daemonsets", err)
		return
	}
	for i := range list.Items {
		d := &list.Items[i]
		if p.excl.IsExcluded(d.Namespace) {
			continue
		}
		b.addWorkload(string(d.UID), "DaemonSet", d.Namespace, d.Name, d.Labels,
			d.Spec.Template.Labels, d.Spec.Template.Spec, nil)
	}
}

func (p *Provider) cronJobs(ctx context.Context, b *builder) {
	var list batchv1.CronJobList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("cronjobs", err)
		return
	}
	for i := range list.Items {
		c := &list.Items[i]
		if p.excl.IsExcluded(c.Namespace) {
			continue
		}
		tmpl := c.Spec.JobTemplate.Spec.Template
		b.addWorkload(string(c.UID), "CronJob", c.Namespace, c.Name, c.Labels,
			tmpl.Labels, tmpl.Spec, nil)
	}
}

func (p *Provider) ingresses(ctx context.Context, b *builder) {
	var list networkingv1.IngressList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("ingresses", err)
		return
	}
	for i := range list.Items {
		ing := &list.Items[i]
		if p.excl.IsExcluded(ing.Namespace) {
			continue
		}
		uid := string(ing.UID)
		var backends []string
		if db := ing.Spec.DefaultBackend; db != nil && db.Service != nil {
			backends = append(backends, db.Service.Name)
		}
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service != nil {
					backends = append(backends, path.Backend.Service.Name)
				}
			}
		}
		b.add(discovery.Asset{
			ExternalID: uid, Provider: providerName, Kind: "ingress",
			Name: ing.Name, Namespace: ing.Namespace, Cluster: p.cluster, Tags: ing.Labels,
			Attributes: map[string]any{"rules": len(ing.Spec.Rules)},
		})
		b.ingresses = append(b.ingresses, ingRef{uid: uid, namespace: ing.Namespace, backends: dedupe(backends)})
	}
}

func (p *Provider) hpas(ctx context.Context, b *builder) {
	var list autoscalingv2.HorizontalPodAutoscalerList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("hpas", err)
		return
	}
	for i := range list.Items {
		h := &list.Items[i]
		if p.excl.IsExcluded(h.Namespace) {
			continue
		}
		uid := string(h.UID)
		ref := h.Spec.ScaleTargetRef
		minReplicas := int32(1)
		if h.Spec.MinReplicas != nil {
			minReplicas = *h.Spec.MinReplicas
		}
		b.add(discovery.Asset{
			ExternalID: uid, Provider: providerName, Kind: "hpa",
			Name: h.Name, Namespace: h.Namespace, Cluster: p.cluster, Tags: h.Labels,
			Attributes: map[string]any{
				"minReplicas": minReplicas, "maxReplicas": h.Spec.MaxReplicas,
				"targetKind": ref.Kind, "targetName": ref.Name, "hasMetrics": len(h.Spec.Metrics) > 0,
			},
		})
		b.hpas = append(b.hpas, hpaRef{uid: uid, namespace: h.Namespace, targetKind: ref.Kind, targetName: ref.Name})
	}
}

func (p *Provider) pdbs(ctx context.Context, b *builder) {
	var list policyv1.PodDisruptionBudgetList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("poddisruptionbudgets", err)
		return
	}
	for i := range list.Items {
		pdb := &list.Items[i]
		if p.excl.IsExcluded(pdb.Namespace) {
			continue
		}
		uid := string(pdb.UID)
		var selector map[string]string
		if pdb.Spec.Selector != nil {
			selector = pdb.Spec.Selector.MatchLabels
		}
		b.add(discovery.Asset{
			ExternalID: uid, Provider: providerName, Kind: "poddisruptionbudget",
			Name: pdb.Name, Namespace: pdb.Namespace, Cluster: p.cluster, Tags: pdb.Labels,
		})
		b.pdbs = append(b.pdbs, pdbRef{uid: uid, namespace: pdb.Namespace, selector: selector})
	}
}

func (p *Provider) networkPolicies(ctx context.Context, b *builder) {
	var list networkingv1.NetworkPolicyList
	if err := p.k8s.List(ctx, &list); err != nil {
		b.fail("networkpolicies", err)
		return
	}
	for i := range list.Items {
		np := &list.Items[i]
		if p.excl.IsExcluded(np.Namespace) {
			continue
		}
		b.add(discovery.Asset{
			ExternalID: string(np.UID), Provider: providerName, Kind: "networkpolicy",
			Name: np.Name, Namespace: np.Namespace, Cluster: p.cluster, Tags: np.Labels,
		})
	}
}
