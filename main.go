package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"sonar-gitlab-commenter/internal/config"
	"sonar-gitlab-commenter/internal/sonar"
)

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

	client := sonar.NewClient(cfg.SonarURL, cfg.SonarToken, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	qualityReport, err := client.FetchQualityReport(ctx, cfg.SonarProjectKey)
	if err != nil {
		if errors.Is(err, sonar.ErrUnauthorized) {
			return fmt.Errorf("failed to authenticate in SonarQube API: %w", err)
		}

		return fmt.Errorf("failed to retrieve SonarQube quality gate and coverage: %w", err)
	}

	fmt.Printf("Fetched %d SonarQube issues with file and line binding for project %q\n", len(issues), cfg.SonarProjectKey)
	fmt.Printf(
		"Quality gate: %s, coverage: %.2f%%, new code coverage: %.2f%%\n",
		qualityReport.QualityGateStatus,
		qualityReport.OverallCoverage,
		qualityReport.NewCodeCoverage,
	)

	return nil
}
