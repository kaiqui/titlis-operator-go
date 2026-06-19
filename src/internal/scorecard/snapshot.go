package scorecard

// WorkloadSnapshot holds normalized metrics extracted from a single Deployment.
// All fields are primitives — no K8s types.
// TenantID is zero here; titlis-api injects the real value before forwarding to scoreops.
type WorkloadSnapshot struct {
	UID         string            `json:"uid"`
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Cluster     string            `json:"cluster"`
	Kind        string            `json:"kind"`
	Criticality string            `json:"criticality"` // "standard" | "high"
	Labels      map[string]string `json:"labels"`
	TenantID    int64             `json:"tenant_id"`
	EngineSlug  string            `json:"engine_slug"`

	HasLivenessProbe         bool    `json:"has_liveness_probe"`
	HasReadinessProbe        bool    `json:"has_readiness_probe"`
	CPURequestSet            bool    `json:"cpu_request_set"`
	CPULimitSet              bool    `json:"cpu_limit_set"`
	MemoryRequestSet         bool    `json:"memory_request_set"`
	MemoryLimitSet           bool    `json:"memory_limit_set"`
	CPULimitRatio            float64 `json:"cpu_limit_ratio"`
	ImageTag                 string  `json:"image_tag"`
	ReadOnlyRootFS           bool    `json:"read_only_root_fs"`
	RunAsNonRoot             bool    `json:"run_as_non_root"`
	AllowPrivilegeEscalation bool    `json:"allow_privilege_escalation"`
	HasDropCapabilities      bool    `json:"has_drop_capabilities"`
	HasPodSecurityContext    bool    `json:"has_pod_security_context"`

	Replicas                  int32  `json:"replicas"`
	Strategy                  string `json:"strategy"`
	TerminationGracePeriodSec int64  `json:"termination_grace_period_sec"`
	HasNetworkPolicy          bool   `json:"has_network_policy"`

	HasHPA                       bool `json:"has_hpa"`
	HPAHasMetrics                bool `json:"hpa_has_metrics"`
	HPAMinReplicas               int  `json:"hpa_min_replicas"`
	HPACPUTargetPercent          int  `json:"hpa_cpu_target_percent"`
	HPAScaleUpStabilizationSec   int  `json:"hpa_scale_up_stabilization_sec"`
	HPAScaleDownStabilizationSec int  `json:"hpa_scale_down_stabilization_sec"`
	HPAHasBehaviorPolicies       bool `json:"hpa_has_behavior_policies"`

	// HasDatadog is true when the Deployment carries the mandatory Datadog Unified Service Tagging
	// labels (tags.datadoghq.com/service). Injected by the operator so titlis-api can pass it to
	// scoreops without an extra DB lookup on the hot scoring path.
	HasDatadog bool `json:"has_datadog,omitempty"`

	// BackstageComponent is the entity name registered in the Backstage catalog, extracted from
	// the Deployment annotation "backstage.io/kubernetes-id" (set by the Backstage K8s plugin)
	// or the fallback "backstage.io/entity-name". Empty when the workload is not in Backstage.
	BackstageComponent string `json:"backstage_component,omitempty"`

	Team        string `json:"team,omitempty"`
	ServiceRepo string `json:"service_repo,omitempty"`
}
