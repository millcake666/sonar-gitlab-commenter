package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"sonar-gitlab-commenter/internal/config"
	"sonar-gitlab-commenter/internal/gitlab"
	"sonar-gitlab-commenter/internal/sonar"
)

const commentMarker = "<!-- sonar-gitlab-commenter -->"
const summaryHeading = "**SonarQube summary**"

var summarySeverityOrder = []string{"BLOCKER", "CRITICAL", "MAJOR", "MINOR", "INFO"}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	return runWith(os.Args[1:], os.Getenv, os.Stdout)
}

func runWith(args []string, getenv func(string) string, stdout io.Writer) error {
	cfg, err := config.Parse(args, getenv)
	if err != nil {
		var helpErr *config.HelpError
		if errors.As(err, &helpErr) {
			if _, writeErr := fmt.Fprint(stdout, helpErr.Message); writeErr != nil {
				return fmt.Errorf("failed to write help output: %w", writeErr)
			}

			return nil
		}

		return err
	}

	gitlabClient := gitlab.NewClient(cfg.GitLabURL, cfg.GitLabToken, nil)
	client := sonar.NewClient(cfg.SonarURL, cfg.SonarToken, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mergeRequest, err := gitlabClient.GetMergeRequest(ctx, cfg.GitLabProjectID, cfg.GitLabMRIID)
	if err != nil {
		if errors.Is(err, gitlab.ErrUnauthorized) {
			return fmt.Errorf("failed to authenticate in GitLab API: %w", err)
		}

		return fmt.Errorf("failed to connect to GitLab API: %w", err)
	}

	if err := client.ValidateAuthentication(ctx); err != nil {
		if errors.Is(err, sonar.ErrUnauthorized) {
			return fmt.Errorf("failed to authenticate in SonarQube API: %w", err)
		}

		return fmt.Errorf("failed to connect to SonarQube API: %w", err)
	}

	issues, err := client.FetchProjectIssues(ctx, cfg.SonarProjectKey)
	if err != nil {
		if errors.Is(err, sonar.ErrUnauthorized) {
			return fmt.Errorf("failed to authenticate in SonarQube API: %w", err)
		}

		return fmt.Errorf("failed to retrieve SonarQube issues: %w", err)
	}
	issues = sonar.FilterIssuesBySeverity(issues, cfg.SeverityThreshold)
	inlineIssues, projectLevelIssues := splitIssuesByLineBinding(issues)

	qualityReport, err := client.FetchQualityReport(ctx, cfg.SonarProjectKey)
	if err != nil {
		if errors.Is(err, sonar.ErrUnauthorized) {
			return fmt.Errorf("failed to authenticate in SonarQube API: %w", err)
		}

		return fmt.Errorf("failed to retrieve SonarQube quality gate and coverage: %w", err)
	}

	resolvedDiscussionsCount := 0
	postedInlineCount := 0
	publishedCommentsCount := 0
	summaryAction := "Skipped (dry-run)"

	if cfg.DryRun {
		if err := writeOutput(stdout, "Dry-run enabled: skipping GitLab discussion resolution and comment publishing\n"); err != nil {
			return err
		}
	} else {
		resolvedDiscussionsCount, err = resolvePreviousSonarDiscussions(
			ctx,
			gitlabClient,
			cfg.GitLabProjectID,
			cfg.GitLabMRIID,
		)
		if err != nil {
			return fmt.Errorf("failed to resolve previous SonarQube discussions: %w", err)
		}

		for _, issue := range inlineIssues {
			if err := gitlabClient.CreateInlineDiscussion(
				ctx,
				cfg.GitLabProjectID,
				cfg.GitLabMRIID,
				formatInlineIssueComment(issue),
				issue.FilePath,
				issue.Line,
				mergeRequest.DiffRefs,
			); err != nil {
				return fmt.Errorf("failed to post inline discussion for SonarQube issue %q: %w", issue.Key, err)
			}
		}

		postedInlineCount = len(inlineIssues)
		publishedCommentsCount = postedInlineCount

		summaryBody := formatMergeRequestSummaryComment(qualityReport, issues, projectLevelIssues)
		summaryUpdated, err := upsertSummaryNote(
			ctx,
			gitlabClient,
			cfg.GitLabProjectID,
			cfg.GitLabMRIID,
			summaryBody,
		)
		if err != nil {
			return fmt.Errorf("failed to post SonarQube summary note: %w", err)
		}
		summaryAction = "Posted"
		if summaryUpdated {
			summaryAction = "Updated"
		} else {
			publishedCommentsCount++
		}
	}

	if err := writeOutput(stdout, "Action log: found %d issues, published %d comments\n", len(issues), publishedCommentsCount); err != nil {
		return err
	}
	if err := writeOutput(
		stdout,
		"Resolved %d previous SonarQube discussions in merge request %d\n",
		resolvedDiscussionsCount,
		cfg.GitLabMRIID,
	); err != nil {
		return err
	}
	if err := writeOutput(
		stdout,
		"Posted %d inline SonarQube discussions to merge request %d\n",
		postedInlineCount,
		cfg.GitLabMRIID,
	); err != nil {
		return err
	}
	if err := writeOutput(
		stdout,
		"%s summary SonarQube note in merge request %d\n",
		summaryAction,
		cfg.GitLabMRIID,
	); err != nil {
		return err
	}
	if err := writeOutput(
		stdout,
		"Quality gate: %s, coverage: %.2f%%, new code coverage: %.2f%%\n",
		qualityReport.QualityGateStatus,
		qualityReport.OverallCoverage,
		qualityReport.NewCodeCoverage,
	); err != nil {
		return err
	}
	if err := writeOutput(stdout, "Resolved GitLab merge request: project_id=%d, mr_iid=%d\n", cfg.GitLabProjectID, cfg.GitLabMRIID); err != nil {
		return err
	}

	return nil
}

func writeOutput(stdout io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(stdout, format, args...); err != nil {
		return fmt.Errorf("failed to write CLI output: %w", err)
	}

	return nil
}

func resolvePreviousSonarDiscussions(
	ctx context.Context,
	gitlabClient *gitlab.Client,
	projectID int,
	mrIID int,
) (int, error) {
	discussions, err := gitlabClient.ListMergeRequestDiscussions(ctx, projectID, mrIID)
	if err != nil {
		return 0, err
	}

	resolvedCount := 0
	for _, discussion := range discussions {
		if discussion.Resolved || !discussion.Resolvable {
			continue
		}
		if !discussionContainsMarker(discussion) {
			continue
		}

		if err := gitlabClient.ResolveMergeRequestDiscussion(ctx, projectID, mrIID, discussion.ID); err != nil {
			return resolvedCount, err
		}
		resolvedCount++
	}

	return resolvedCount, nil
}

func discussionContainsMarker(discussion gitlab.Discussion) bool {
	for _, note := range discussion.Notes {
		if commentHasMarker(note.Body) {
			return true
		}
	}

	return false
}

func upsertSummaryNote(
	ctx context.Context,
	gitlabClient *gitlab.Client,
	projectID int,
	mrIID int,
	body string,
) (bool, error) {
	notes, err := gitlabClient.ListMergeRequestNotes(ctx, projectID, mrIID)
	if err != nil {
		return false, err
	}

	summaryNote, found := findLatestSummaryNote(notes)
	if !found {
		if err := gitlabClient.CreateMergeRequestNote(ctx, projectID, mrIID, body); err != nil {
			return false, err
		}
		return false, nil
	}

	if err := gitlabClient.UpdateMergeRequestNote(ctx, projectID, mrIID, summaryNote.ID, body); err != nil {
		return false, err
	}

	return true, nil
}

func findLatestSummaryNote(notes []gitlab.MergeRequestNote) (gitlab.MergeRequestNote, bool) {
	var (
		found     bool
		latestOne gitlab.MergeRequestNote
	)

	for _, note := range notes {
		if !isSummaryNote(note.Body) {
			continue
		}
		if !found || note.ID > latestOne.ID {
			found = true
			latestOne = note
		}
	}

	return latestOne, found
}

func isSummaryNote(body string) bool {
	return commentHasMarker(body) && strings.Contains(body, summaryHeading)
}

func commentHasMarker(body string) bool {
	return strings.Contains(body, commentMarker)
}

func splitIssuesByLineBinding(issues []sonar.Issue) ([]sonar.Issue, []sonar.Issue) {
	inlineIssues := make([]sonar.Issue, 0, len(issues))
	projectLevelIssues := make([]sonar.Issue, 0)

	for _, issue := range issues {
		if strings.TrimSpace(issue.FilePath) == "" || issue.Line <= 0 {
			projectLevelIssues = append(projectLevelIssues, issue)
			continue
		}

		inlineIssues = append(inlineIssues, issue)
	}

	return inlineIssues, projectLevelIssues
}

func formatInlineIssueComment(issue sonar.Issue) string {
	return fmt.Sprintf(
		"%s\n**SonarQube issue**\n- Severity: `%s`\n- Type: `%s`\n- Message: %s\n- Rule key: `%s`",
		commentMarker,
		strings.TrimSpace(issue.Severity),
		strings.TrimSpace(issue.Type),
		strings.TrimSpace(issue.Message),
		strings.TrimSpace(issue.Rule),
	)
}

func formatMergeRequestSummaryComment(
	qualityReport sonar.QualityReport,
	issues []sonar.Issue,
	projectLevelIssues []sonar.Issue,
) string {
	issuesBySeverity, unknownSeverityCount := countIssuesBySeverity(issues)

	var builder strings.Builder
	builder.WriteString(commentMarker)
	builder.WriteString("\n")
	builder.WriteString(summaryHeading)
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("- Quality gate: %s\n", formatQualityGateStatus(qualityReport.QualityGateStatus)))
	builder.WriteString(fmt.Sprintf("- Overall coverage: %.2f%%\n", qualityReport.OverallCoverage))
	builder.WriteString(fmt.Sprintf("- New code coverage: %.2f%%\n", qualityReport.NewCodeCoverage))
	builder.WriteString(fmt.Sprintf("- Total issues: %d\n", len(issues)))
	builder.WriteString("\n**Issues by severity**\n")
	for _, severity := range summarySeverityOrder {
		builder.WriteString(fmt.Sprintf("- %s: %d\n", severity, issuesBySeverity[severity]))
	}
	if unknownSeverityCount > 0 {
		builder.WriteString(fmt.Sprintf("- UNKNOWN: %d\n", unknownSeverityCount))
	}

	if len(projectLevelIssues) > 0 {
		builder.WriteString("\n**SonarQube issues without line binding**\n")
		for i, issue := range projectLevelIssues {
			builder.WriteString(
				fmt.Sprintf(
					"%d. [%s][%s] %s (rule `%s`)\n",
					i+1,
					strings.TrimSpace(issue.Severity),
					strings.TrimSpace(issue.Type),
					strings.TrimSpace(issue.Message),
					strings.TrimSpace(issue.Rule),
				),
			)
		}
	}

	return strings.TrimRight(builder.String(), "\n")
}

func countIssuesBySeverity(issues []sonar.Issue) (map[string]int, int) {
	counts := make(map[string]int, len(sonar.AllowedSeverities()))
	for _, severity := range sonar.AllowedSeverities() {
		counts[severity] = 0
	}

	unknownSeverityCount := 0
	for _, issue := range issues {
		normalizedSeverity := sonar.NormalizeSeverity(issue.Severity)
		if _, ok := counts[normalizedSeverity]; !ok {
			unknownSeverityCount++
			continue
		}

		counts[normalizedSeverity]++
	}

	return counts, unknownSeverityCount
}

func formatQualityGateStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "passed":
		return "✅ **passed**"
	case "failed":
		return "❌ **failed**"
	default:
		return "⚠️ **warning**"
	}
}
