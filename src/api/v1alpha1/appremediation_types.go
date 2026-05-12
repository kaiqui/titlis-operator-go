package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="PR",type=integer,JSONPath=`.status.prNumber`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type AppRemediation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AppRemediationSpec   `json:"spec,omitempty"`
	Status            AppRemediationStatus `json:"status,omitempty"`
}

type AppRemediationSpec struct {
	TargetRef   TargetRef `json:"targetRef"`
	IssuesFixed []string  `json:"issuesFixed,omitempty"`
	BaseBranch  string    `json:"baseBranch,omitempty"`
}

type AppRemediationStatus struct {
	Phase      string             `json:"phase,omitempty"` // PRCreated | PRMerged | PRClosed | Failed
	PRNumber   int                `json:"prNumber,omitempty"`
	PRURL      string             `json:"prUrl,omitempty"`
	PRBranch   string             `json:"prBranch,omitempty"`
	IssueCount int                `json:"issueCount,omitempty"`
	Error      string             `json:"error,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type AppRemediationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppRemediation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppRemediation{}, &AppRemediationList{})
}
