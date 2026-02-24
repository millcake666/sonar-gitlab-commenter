package main

import (
	"strings"
	"testing"

	"sonar-gitlab-commenter/internal/sonar"
)

func TestFormatMergeRequestSummaryComment(t *testing.T) {
	t.Parallel()

	issues := []sonar.Issue{
		{Key: "A", Severity: "major"},
		{Key: "B", Severity: "CRITICAL"},
		{Key: "C", Severity: "BLOCKER"},
		{Key: "D", Severity: "unknown"},
	}
	projectLevelIssues := []sonar.Issue{
		{
			Severity: "MAJOR",
			Type:     "CODE_SMELL",
			Message:  "Project level issue",
			Rule:     "go:S100",
		},
	}

	comment := formatMergeRequestSummaryComment(
		sonar.QualityReport{
			QualityGateStatus: "passed",
			OverallCoverage:   82.4,
			NewCodeCoverage:   75.1,
		},
		issues,
		projectLevelIssues,
	)

	assertCommentContains(t, comment, commentMarker)
	assertCommentContains(t, comment, "Quality gate: ✅ **passed**")
	assertCommentContains(t, comment, "Overall coverage: 82.40%")
	assertCommentContains(t, comment, "New code coverage: 75.10%")
	assertCommentContains(t, comment, "Total issues: 4")
	assertCommentContains(t, comment, "BLOCKER: 1")
	assertCommentContains(t, comment, "CRITICAL: 1")
	assertCommentContains(t, comment, "MAJOR: 1")
	assertCommentContains(t, comment, "MINOR: 0")
	assertCommentContains(t, comment, "INFO: 0")
	assertCommentContains(t, comment, "UNKNOWN: 1")
	assertCommentContains(t, comment, "**SonarQube issues without line binding**")
	assertCommentContains(t, comment, "1. [MAJOR][CODE_SMELL] Project level issue (rule `go:S100`)")
}

func TestFormatMergeRequestSummaryCommentWithoutProjectLevelIssues(t *testing.T) {
	t.Parallel()

	comment := formatMergeRequestSummaryComment(
		sonar.QualityReport{QualityGateStatus: "failed"},
		[]sonar.Issue{{Severity: "MINOR"}},
		nil,
	)

	assertCommentContains(t, comment, "Quality gate: ❌ **failed**")
	assertCommentContains(t, comment, "MINOR: 1")
	if strings.Contains(comment, "without line binding") {
		t.Fatalf("did not expect project-level section, got %q", comment)
	}
}

func assertCommentContains(t *testing.T, comment, expected string) {
	t.Helper()

	if !strings.Contains(comment, expected) {
		t.Fatalf("comment %q does not contain %q", comment, expected)
	}
}
