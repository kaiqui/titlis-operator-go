package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Score",type=integer,JSONPath=`.status.overallScore`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.complianceStatus`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type AppScorecard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AppScorecardSpec   `json:"spec,omitempty"`
	Status            AppScorecardStatus `json:"status,omitempty"`
}

type TargetRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type AppScorecardSpec struct {
	TargetRef TargetRef `json:"targetRef"`
}

type FindingResult struct {
	RuleID      string `json:"ruleId"`
	Pillar      string `json:"pillar"`
	Severity    string `json:"severity"`
	Passed      bool   `json:"passed"`
	Message     string `json:"message"`
	ActualValue string `json:"actual,omitempty"`
}

type PillarResult struct {
	Name         string `json:"name"`
	Score        int    `json:"score"`
	PassedChecks int    `json:"passedChecks"`
	TotalChecks  int    `json:"totalChecks"`
}

type RemediationInfo struct {
	PRNumber int    `json:"prNumber,omitempty"`
	PRURL    string `json:"prUrl,omitempty"`
	Status   string `json:"status,omitempty"`
}

type AppScorecardStatus struct {
	OverallScore     *int32             `json:"overallScore,omitempty"`
	ComplianceStatus string             `json:"complianceStatus,omitempty"`
	CriticalIssues   int                `json:"criticalIssues,omitempty"`
	ErrorIssues      int                `json:"errorIssues,omitempty"`
	WarningIssues    int                `json:"warningIssues,omitempty"`
	Pillars          []PillarResult     `json:"pillars,omitempty"`
	Findings         []FindingResult    `json:"findings,omitempty"`
	Remediation      *RemediationInfo   `json:"remediation,omitempty"`
	LastNotification *metav1.Time       `json:"lastNotification,omitempty"`
	LastEvaluatedAt  *metav1.Time       `json:"lastEvaluatedAt,omitempty"`
	Conditions       []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type AppScorecardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppScorecard `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppScorecard{}, &AppScorecardList{})
}
