package discovery

// Correlator produces cross-provider edges from the merged asset list (after every provider ran).
// It only discovers relations — it never scores (regra 14). New correlators (cloud↔k8s, repo↔service)
// follow this shape.
type Correlator interface {
	Correlate(assets []Asset) []Relation
}

// ServiceCorrelator links a Datadog service to the Kubernetes workload that implements it.
// Primary signal: Unified Service Tagging (`tags.datadoghq.com/service`) surfaced by the
// KubernetesProvider as the workload attribute "ddService". Fallback: exact name match.
type ServiceCorrelator struct{}

func (ServiceCorrelator) Correlate(assets []Asset) []Relation {
	ddServiceByName := map[string]string{} // dd_service Name → externalId
	for i := range assets {
		a := &assets[i]
		if a.Provider == "datadog" && a.Kind == "dd_service" {
			ddServiceByName[a.Name] = a.ExternalID
		}
	}
	if len(ddServiceByName) == 0 {
		return nil
	}

	var rels []Relation
	seen := map[string]bool{}
	for i := range assets {
		a := &assets[i]
		if a.Provider != "kubernetes" || !isWorkloadKind(a.Kind) {
			continue
		}

		svc, _ := a.Attributes["ddService"].(string)
		if svc == "" {
			if _, ok := ddServiceByName[a.Name]; ok {
				svc = a.Name // fallback: workload name == dd_service name
			}
		}
		if svc == "" {
			continue
		}
		ddExt, ok := ddServiceByName[svc]
		if !ok {
			continue
		}

		key := ddExt + "->" + a.ExternalID
		if seen[key] {
			continue
		}
		seen[key] = true
		rels = append(rels, Relation{
			SourceExternalID: ddExt, SourceProvider: "datadog",
			TargetExternalID: a.ExternalID, TargetProvider: "kubernetes",
			Type: "describes",
		})
	}
	return rels
}

func isWorkloadKind(kind string) bool {
	switch kind {
	case "deployment", "statefulset", "daemonset", "cronjob":
		return true
	default:
		return false
	}
}
