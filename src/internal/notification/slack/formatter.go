package slack

import (
	"fmt"
	"sort"
	"strings"

	"github.com/titlis/operator/internal/model"
	"github.com/titlis/operator/internal/notification"
)

func scoreEmoji(score float64) string {
	switch {
	case score >= 90:
		return "🟢"
	case score >= 80:
		return "🟡"
	case score >= 70:
		return "🟠"
	default:
		return "🔴"
	}
}

func formatDigest(ns string, scorecards []model.ResourceScorecard) (title, message string, sev notification.Severity) {
	var totalCritical, totalErrors, totalWarnings int
	var minScore float64 = 100

	for _, sc := range scorecards {
		totalCritical += sc.CriticalIssues
		totalErrors += sc.ErrorIssues
		totalWarnings += sc.WarningIssues
		if sc.OverallScore < minScore {
			minScore = sc.OverallScore
		}
	}

	switch {
	case totalCritical > 0 || minScore < 70:
		sev = notification.SeverityCritical
		title = fmt.Sprintf("🔴 Scorecard Digest — namespace: %s", ns)
	case totalErrors > 0 || minScore < 80:
		sev = notification.SeverityError
		title = fmt.Sprintf("🟠 Scorecard Digest — namespace: %s", ns)
	case totalWarnings > 0 || minScore < 90:
		sev = notification.SeverityWarning
		title = fmt.Sprintf("🟡 Scorecard Digest — namespace: %s", ns)
	default:
		sev = notification.SeverityInfo
		title = fmt.Sprintf("🟢 Scorecard Digest — namespace: %s", ns)
	}

	sorted := make([]model.ResourceScorecard, len(scorecards))
	copy(sorted, scorecards)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].CriticalIssues != sorted[j].CriticalIssues {
			return sorted[i].CriticalIssues > sorted[j].CriticalIssues
		}
		if sorted[i].ErrorIssues != sorted[j].ErrorIssues {
			return sorted[i].ErrorIssues > sorted[j].ErrorIssues
		}
		return sorted[i].OverallScore < sorted[j].OverallScore
	})

	var sb strings.Builder
	for _, sc := range sorted {
		issues := ""
		if sc.CriticalIssues+sc.ErrorIssues+sc.WarningIssues > 0 {
			issues = fmt.Sprintf("%dc %de %dw", sc.CriticalIssues, sc.ErrorIssues, sc.WarningIssues)
		}
		sb.WriteString(fmt.Sprintf("%s `%-35s` %5.1f/100  %s\n",
			scoreEmoji(sc.OverallScore), sc.ResourceName, sc.OverallScore, issues))
	}

	// Top 5 worst findings
	type finding struct {
		app  string
		rule string
		msg  string
	}
	var top []finding
	for _, sc := range sorted {
		if len(top) >= 5 {
			break
		}
		for pillar, ps := range sc.PillarScores {
			_ = pillar
			for _, r := range ps.ValidationResults {
				if !r.Passed && (r.Severity == model.SeverityCritical || r.Severity == model.SeverityError) {
					top = append(top, finding{app: sc.ResourceName, rule: r.RuleID, msg: r.RuleName})
					if len(top) >= 5 {
						break
					}
				}
			}
			if len(top) >= 5 {
				break
			}
		}
	}
	if len(top) > 0 {
		sb.WriteString("\n*Issues críticos/erros:*\n")
		for _, f := range top {
			sb.WriteString(fmt.Sprintf("• *%s*: [%s] %s\n", f.app, f.rule, f.msg))
		}
	}

	sb.WriteString(fmt.Sprintf("\n`kubectl get appscorecard -n %s`", ns))

	message = sb.String()
	if len(message) > 3000 {
		message = message[:2997] + "..."
	}
	return
}
