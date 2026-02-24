package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"sonar-gitlab-commenter/internal/config"
	"sonar-gitlab-commenter/internal/gitlab"
	"sonar-gitlab-commenter/internal/sonar"
)

const commentMarker = "<!-- sonar-gitlab-commenter -->"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse(os.Args[1:], os.Getenv)
	if err != nil {
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

	if len(projectLevelIssues) > 0 {
		if err := gitlabClient.CreateMergeRequestNote(
			ctx,
			cfg.GitLabProjectID,
			cfg.GitLabMRIID,
			formatProjectLevelSummaryComment(projectLevelIssues),
		); err != nil {
			return fmt.Errorf("failed to post project-level SonarQube summary: %w", err)
		}
	}

	qualityReport, err := client.FetchQualityReport(ctx, cfg.SonarProjectKey)
	if err != nil {
		if errors.Is(err, sonar.ErrUnauthorized) {
			return fmt.Errorf("failed to authenticate in SonarQube API: %w", err)
		}

		return fmt.Errorf("failed to retrieve SonarQube quality gate and coverage: %w", err)
	}

	fmt.Printf("Fetched %d SonarQube issues for project %q\n", len(issues), cfg.SonarProjectKey)
	fmt.Printf("Posted %d inline SonarQube discussions to merge request %d\n", len(inlineIssues), cfg.GitLabMRIID)
	if len(projectLevelIssues) > 0 {
		fmt.Printf("Posted summary note for %d project-level SonarQube issues\n", len(projectLevelIssues))
	}
	fmt.Printf(
		"Quality gate: %s, coverage: %.2f%%, new code coverage: %.2f%%\n",
		qualityReport.QualityGateStatus,
		qualityReport.OverallCoverage,
		qualityReport.NewCodeCoverage,
	)
	fmt.Printf("Resolved GitLab merge request: project_id=%d, mr_iid=%d\n", cfg.GitLabProjectID, cfg.GitLabMRIID)

	return nil
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

func formatProjectLevelSummaryComment(issues []sonar.Issue) string {
	var builder strings.Builder
	builder.WriteString(commentMarker)
	builder.WriteString("\n**SonarQube issues without line binding**\n")

	for i, issue := range issues {
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

	return strings.TrimRight(builder.String(), "\n")
}
