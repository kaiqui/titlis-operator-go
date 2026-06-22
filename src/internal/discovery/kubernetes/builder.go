package kubernetes

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/titlis/operator/internal/discovery"
)

// workloadRef holds the data needed to build edges for a workload after all resources are listed.
type workloadRef struct {
	uid            string
	namespace      string
	name           string
	kind           string
	templateLabels map[string]string
	cmRefs         []string
	secretRefs     []string
}

type svcRef struct {
	uid       string
	namespace string
	selector  map[string]string
}

type ingRef struct {
	uid       string
	namespace string
	backends  []string
}

type hpaRef struct {
	uid        string
	namespace  string
	targetKind string
	targetName string
}

type pdbRef struct {
	uid       string
	namespace string
	selector  map[string]string
}

// builder accumulates assets, relations, name→UID indexes and edge inputs across one sweep.
type builder struct {
	cluster   string
	assets    []discovery.Asset
	relations []discovery.Relation

	workloads []workloadRef
	services  []svcRef
	ingresses []ingRef
	hpas      []hpaRef
	pdbs      []pdbRef

	serviceUID   map[string]string // ns/name → uid
	configMapUID map[string]string
	secretUID    map[string]string

	listErrors []string
}

func newBuilder(cluster string) *builder {
	return &builder{
		cluster:      cluster,
		serviceUID:   map[string]string{},
		configMapUID: map[string]string{},
		secretUID:    map[string]string{},
	}
}

func (b *builder) add(a discovery.Asset) { b.assets = append(b.assets, a) }

func (b *builder) fail(resource string, err error) {
	b.listErrors = append(b.listErrors, fmt.Sprintf("list %s: %v", resource, err))
}

func (b *builder) rel(src, tgt, typ string) {
	b.relations = append(b.relations, discovery.Relation{
		SourceExternalID: src, SourceProvider: providerName,
		TargetExternalID: tgt, TargetProvider: providerName, Type: typ,
	})
}

func (b *builder) addWorkload(uid, kind, namespace, name string, labels, templateLabels map[string]string, spec corev1.PodSpec, replicas *int32) {
	cm, sec := podRefs(spec)
	attrs := workloadAttributes(spec, replicas)
	// Surface Unified Service Tagging so the cross-provider correlator can link this workload to
	// its Datadog service without re-reading template labels.
	if dd := ustagService(labels, templateLabels); dd != "" {
		attrs["ddService"] = dd
	}
	b.add(discovery.Asset{
		ExternalID: uid, Provider: providerName, Kind: workloadKind(kind),
		Name: name, Namespace: namespace, Cluster: b.cluster, Tags: labels,
		Attributes: attrs,
	})
	b.workloads = append(b.workloads, workloadRef{
		uid: uid, namespace: namespace, name: name, kind: kind,
		templateLabels: templateLabels, cmRefs: cm, secretRefs: sec,
	})
}

const ustagServiceKey = "tags.datadoghq.com/service"

// ustagService reads the Datadog Unified Service Tagging service name from the workload label,
// falling back to the pod template label.
func ustagService(labels, templateLabels map[string]string) string {
	if v := labels[ustagServiceKey]; v != "" {
		return v
	}
	return templateLabels[ustagServiceKey]
}

// resolveEdges runs once all resources are listed, turning collected refs into relations.
func (b *builder) resolveEdges() {
	for _, svc := range b.services {
		for _, wl := range b.workloads {
			if wl.namespace == svc.namespace && labelsSubset(svc.selector, wl.templateLabels) {
				b.rel(svc.uid, wl.uid, "selects")
			}
		}
	}
	for _, ing := range b.ingresses {
		for _, name := range ing.backends {
			if uid, ok := b.serviceUID[nn(ing.namespace, name)]; ok {
				b.rel(ing.uid, uid, "routes_to")
			}
		}
	}
	for _, h := range b.hpas {
		for _, wl := range b.workloads {
			if wl.namespace == h.namespace && wl.kind == h.targetKind && wl.name == h.targetName {
				b.rel(wl.uid, h.uid, "scaled_by")
			}
		}
	}
	for _, pdb := range b.pdbs {
		for _, wl := range b.workloads {
			if wl.namespace == pdb.namespace && labelsSubset(pdb.selector, wl.templateLabels) {
				b.rel(wl.uid, pdb.uid, "protected_by")
			}
		}
	}
	for _, wl := range b.workloads {
		for _, name := range wl.cmRefs {
			if uid, ok := b.configMapUID[nn(wl.namespace, name)]; ok {
				b.rel(wl.uid, uid, "uses_config")
			}
		}
		for _, name := range wl.secretRefs {
			if uid, ok := b.secretUID[nn(wl.namespace, name)]; ok {
				b.rel(wl.uid, uid, "uses_secret")
			}
		}
	}
}

// --- helpers ---

func nn(namespace, name string) string { return namespace + "/" + name }

func workloadKind(kind string) string {
	switch kind {
	case "Deployment":
		return "deployment"
	case "StatefulSet":
		return "statefulset"
	case "DaemonSet":
		return "daemonset"
	case "CronJob":
		return "cronjob"
	default:
		return kind
	}
}

func labelsSubset(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// podRefs collects ConfigMap and Secret names referenced by a pod spec (env, envFrom, volumes).
func podRefs(spec corev1.PodSpec) (cmRefs, secretRefs []string) {
	containers := append([]corev1.Container{}, spec.InitContainers...)
	containers = append(containers, spec.Containers...)
	for _, c := range containers {
		for _, ef := range c.EnvFrom {
			if ef.ConfigMapRef != nil {
				cmRefs = append(cmRefs, ef.ConfigMapRef.Name)
			}
			if ef.SecretRef != nil {
				secretRefs = append(secretRefs, ef.SecretRef.Name)
			}
		}
		for _, e := range c.Env {
			if e.ValueFrom == nil {
				continue
			}
			if e.ValueFrom.ConfigMapKeyRef != nil {
				cmRefs = append(cmRefs, e.ValueFrom.ConfigMapKeyRef.Name)
			}
			if e.ValueFrom.SecretKeyRef != nil {
				secretRefs = append(secretRefs, e.ValueFrom.SecretKeyRef.Name)
			}
		}
	}
	for _, v := range spec.Volumes {
		if v.ConfigMap != nil {
			cmRefs = append(cmRefs, v.ConfigMap.Name)
		}
		if v.Secret != nil {
			secretRefs = append(secretRefs, v.Secret.SecretName)
		}
	}
	return dedupe(cmRefs), dedupe(secretRefs)
}

func workloadAttributes(spec corev1.PodSpec, replicas *int32) map[string]any {
	attrs := map[string]any{}
	if replicas != nil {
		attrs["replicas"] = *replicas
	}
	images := make([]string, 0, len(spec.Containers))
	for _, c := range spec.Containers {
		images = append(images, c.Image)
	}
	attrs["images"] = images
	if len(spec.Containers) > 0 {
		c := spec.Containers[0]
		attrs["hasLivenessProbe"] = c.LivenessProbe != nil
		attrs["hasReadinessProbe"] = c.ReadinessProbe != nil
		attrs["cpuRequestSet"] = !c.Resources.Requests.Cpu().IsZero()
		attrs["cpuLimitSet"] = !c.Resources.Limits.Cpu().IsZero()
		attrs["memoryRequestSet"] = !c.Resources.Requests.Memory().IsZero()
		attrs["memoryLimitSet"] = !c.Resources.Limits.Memory().IsZero()
		if csc := c.SecurityContext; csc != nil {
			if csc.RunAsNonRoot != nil {
				attrs["runAsNonRoot"] = *csc.RunAsNonRoot
			}
			if csc.ReadOnlyRootFilesystem != nil {
				attrs["readOnlyRootFilesystem"] = *csc.ReadOnlyRootFilesystem
			}
		}
	}
	return attrs
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
