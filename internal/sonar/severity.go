package sonar

import "strings"

var severityRanks = map[string]int{
	"INFO":     0,
	"MINOR":    1,
	"MAJOR":    2,
	"CRITICAL": 3,
	"BLOCKER":  4,
}

var severityOrder = []string{"INFO", "MINOR", "MAJOR", "CRITICAL", "BLOCKER"}

func AllowedSeverities() []string {
	allowed := make([]string, len(severityOrder))
	copy(allowed, severityOrder)

	return allowed
}

func NormalizeSeverity(severity string) string {
	return strings.ToUpper(strings.TrimSpace(severity))
}

func IsValidSeverity(severity string) bool {
	_, ok := severityRanks[NormalizeSeverity(severity)]
	return ok
}

func FilterIssuesBySeverity(issues []Issue, threshold string) []Issue {
	normalizedThreshold := NormalizeSeverity(threshold)
	if normalizedThreshold == "" {
		all := make([]Issue, len(issues))
		copy(all, issues)
		return all
	}

	thresholdRank, ok := severityRanks[normalizedThreshold]
	if !ok {
		all := make([]Issue, len(issues))
		copy(all, issues)
		return all
	}

	filtered := make([]Issue, 0, len(issues))
	for _, issue := range issues {
		issueRank, issueSeverityKnown := severityRanks[NormalizeSeverity(issue.Severity)]
		if !issueSeverityKnown || issueRank < thresholdRank {
			continue
		}

		filtered = append(filtered, issue)
	}

	return filtered
}
