package notification

import "context"

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

type BatchItem struct {
	Name         string
	OverallScore float64
	Critical     int
	Errors       int
	Warnings     int
}

// Notifier — interface extensível: Slack hoje, Teams/PagerDuty amanhã.
type Notifier interface {
	Send(ctx context.Context, channel, title, message string, severity Severity) error
	SendBatch(ctx context.Context, namespace string, items []BatchItem, severity Severity) error
}
