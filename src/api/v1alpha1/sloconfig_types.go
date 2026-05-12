package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Service",type=string,JSONPath=`.spec.service`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type SLOConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SLOConfigSpec   `json:"spec,omitempty"`
	Status            SLOConfigStatus `json:"status,omitempty"`
}

type SLOConfigSpec struct {
	Service             string   `json:"service"`
	Type                string   `json:"type,omitempty"`          // metric | monitor | time_slice
	AppFramework        *string  `json:"app_framework,omitempty"` // wsgi | fastapi | aiohttp
	Target              float64  `json:"target,omitempty"`
	Warning             *float64 `json:"warning,omitempty"`
	Timeframe           string   `json:"timeframe,omitempty"` // 7d | 30d | 90d
	Numerator           *string  `json:"numerator,omitempty"`
	Denominator         *string  `json:"denominator,omitempty"`
	Tags                []string `json:"tags,omitempty"`
	AutoDetectFramework bool     `json:"auto_detect_framework,omitempty"`
}

type SLOConfigStatus struct {
	SLOID             *string            `json:"slo_id,omitempty"`
	State             string             `json:"state,omitempty"` // pending | synced | error
	LastSync          *metav1.Time       `json:"last_sync,omitempty"`
	Error             *string            `json:"error,omitempty"`
	DetectedFramework *string            `json:"detected_framework,omitempty"`
	Conditions        []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type SLOConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SLOConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SLOConfig{}, &SLOConfigList{})
}
