package model

import "time"

type ComplianceStatus string

const (
	ComplianceCompliant    ComplianceStatus = "compliant"
	ComplianceNonCompliant ComplianceStatus = "non_compliant"
	ComplianceUnknown      ComplianceStatus = "unknown"
	CompliancePending      ComplianceStatus = "pending"
)

type CriticalityLevel string

const (
	CriticalityStandard CriticalityLevel = "standard"
	CriticalityHigh     CriticalityLevel = "high"
)

type ValidationPillar string

const (
	PillarResilience  ValidationPillar = "resilience"
	PillarSecurity    ValidationPillar = "security"
	PillarCost        ValidationPillar = "cost"
	PillarPerformance ValidationPillar = "performance"
	PillarOperational ValidationPillar = "operational"
	PillarCompliance  ValidationPillar = "compliance"
)

type ValidationRuleType string

const (
	RuleTypeBoolean ValidationRuleType = "boolean"
	RuleTypeNumeric ValidationRuleType = "numeric"
	RuleTypeEnum    ValidationRuleType = "enum"
	RuleTypeRegex   ValidationRuleType = "regex"
)

type ValidationSeverity string

const (
	SeverityCritical ValidationSeverity = "critical"
	SeverityError    ValidationSeverity = "error"
	SeverityWarning  ValidationSeverity = "warning"
	SeverityInfo     ValidationSeverity = "info"
	SeverityOptional ValidationSeverity = "optional"
)

type ValidationRule struct {
	ID                 string
	Pillar             ValidationPillar
	Name               string
	Description        string
	RuleType           ValidationRuleType
	Weight             float64
	Severity           ValidationSeverity
	Enabled            bool
	AppliesTo          []string
	MinValue           *float64
	MaxValue           *float64
	RegexPattern       *string
	CriticalityProfile *string // nil = todos; "high" = só annotation high
	Remediation        *string
	DocumentationURL   *string
}

type ValidationResult struct {
	RuleID           string             `json:"rule_id"`
	RuleName         string             `json:"rule_name"`
	Pillar           ValidationPillar   `json:"pillar"`
	Passed           bool               `json:"passed"`
	Severity         ValidationSeverity `json:"severity"`
	RuleType         ValidationRuleType `json:"rule_type"`
	Weight           float64            `json:"weight"`
	Message          string             `json:"message"`
	ActualValue      *string            `json:"actual_value,omitempty"`
	IsRemediable     bool               `json:"is_remediable"`
	Remediation      *string            `json:"remediation,omitempty"`
	DocumentationURL *string            `json:"documentation_url,omitempty"`
	Timestamp        time.Time          `json:"timestamp"`
}

type PillarScore struct {
	Pillar            ValidationPillar   `json:"pillar"`
	Score             float64            `json:"score"`          // 0-100
	MaxScore          float64            `json:"max_score"`      // sempre 100.0
	PassedChecks      int                `json:"passed_checks"`
	TotalChecks       int                `json:"total_checks"`
	WeightedScore     float64            `json:"weighted_score"` // soma dos pesos das regras que passaram
	ValidationResults []ValidationResult `json:"-"`              // interno; omitir em JSON externo
}

type ResourceScorecard struct {
	ResourceName      string                           `json:"resource_name"`
	ResourceNamespace string                           `json:"resource_namespace"`
	ResourceKind      string                           `json:"resource_kind"`
	ResourceUID       string                           `json:"resource_uid,omitempty"`
	PillarScores      map[ValidationPillar]PillarScore `json:"pillar_scores"`
	OverallScore      float64                          `json:"overall_score"`
	CriticalIssues    int                              `json:"critical_issues"`
	ErrorIssues       int                              `json:"error_issues"`
	WarningIssues     int                              `json:"warning_issues"`
	PassedChecks      int                              `json:"passed_checks"`
	TotalChecks       int                              `json:"total_checks"`
	Timestamp         time.Time                        `json:"timestamp"`
	CriticalityLevel  CriticalityLevel                 `json:"criticality_level"`
}

type ScorecardConfig struct {
	Rules                   []ValidationRule
	NotifyCriticalThreshold float64
	NotifyErrorThreshold    float64
	NotifyWarningThreshold  float64
	NotificationCooldown    time.Duration
	BatchIntervalMinutes    int
	BatchSize               int
	ExcludedNamespaces      []string
}

// PillarWeights — soma = 110.0, não 100.0.
// Divisor em calculateOverallScore é a soma dos pesos dos pilares presentes.
var PillarWeights = map[ValidationPillar]float64{
	PillarResilience:  30.0,
	PillarSecurity:    25.0,
	PillarCompliance:  20.0,
	PillarPerformance: 15.0,
	PillarOperational: 10.0,
	PillarCost:        10.0,
}
