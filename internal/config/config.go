package config

import (
	"flag"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"sonar-gitlab-commenter/internal/sonar"
)

type Config struct {
	SonarURL          string
	SonarToken        string
	SonarProjectKey   string
	SeverityThreshold string
	GitLabURL         string
	GitLabToken       string
	GitLabProjectID   int
	GitLabMRIID       int
}

func Parse(args []string, getenv func(string) string) (Config, error) {
	cfg := Config{
		SonarURL:        strings.TrimSpace(getenv("SONAR_HOST_URL")),
		SonarToken:      strings.TrimSpace(getenv("SONAR_TOKEN")),
		SonarProjectKey: strings.TrimSpace(getenv("SONAR_PROJECT_KEY")),
		GitLabURL:       strings.TrimSpace(getenv("GITLAB_URL")),
		GitLabToken:     strings.TrimSpace(getenv("GITLAB_TOKEN")),
	}
	projectID := strings.TrimSpace(getenv("CI_PROJECT_ID"))
	mrIID := strings.TrimSpace(getenv("CI_MERGE_REQUEST_IID"))

	fs := flag.NewFlagSet("sonar-gitlab-commenter", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&cfg.SonarURL, "sonar-url", cfg.SonarURL, "SonarQube server URL (env: SONAR_HOST_URL)")
	fs.StringVar(&cfg.SonarToken, "sonar-token", cfg.SonarToken, "SonarQube access token (env: SONAR_TOKEN)")
	fs.StringVar(&cfg.SonarProjectKey, "sonar-project-key", cfg.SonarProjectKey, "SonarQube project key (env: SONAR_PROJECT_KEY)")
	fs.StringVar(&cfg.SeverityThreshold, "severity-threshold", "", "Minimum SonarQube issue severity to include (INFO, MINOR, MAJOR, CRITICAL, BLOCKER)")
	fs.StringVar(&cfg.GitLabURL, "gitlab-url", cfg.GitLabURL, "GitLab server URL (env: GITLAB_URL)")
	fs.StringVar(&cfg.GitLabToken, "gitlab-token", cfg.GitLabToken, "GitLab access token (env: GITLAB_TOKEN)")
	fs.StringVar(&projectID, "project-id", projectID, "GitLab project ID (env: CI_PROJECT_ID)")
	fs.StringVar(&mrIID, "mr-iid", mrIID, "GitLab merge request IID (env: CI_MERGE_REQUEST_IID)")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("invalid CLI arguments: %w", err)
	}

	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}

	cfg.SonarURL = strings.TrimSpace(cfg.SonarURL)
	cfg.SonarToken = strings.TrimSpace(cfg.SonarToken)
	cfg.SonarProjectKey = strings.TrimSpace(cfg.SonarProjectKey)
	cfg.GitLabURL = strings.TrimSpace(cfg.GitLabURL)
	cfg.GitLabToken = strings.TrimSpace(cfg.GitLabToken)
	projectID = strings.TrimSpace(projectID)
	mrIID = strings.TrimSpace(mrIID)
	cfg.SeverityThreshold = sonar.NormalizeSeverity(cfg.SeverityThreshold)

	if missing := missingSonarFields(cfg); len(missing) > 0 {
		return Config{}, fmt.Errorf(
			"missing required SonarQube configuration: %s (set env vars SONAR_HOST_URL/SONAR_TOKEN/SONAR_PROJECT_KEY or flags --sonar-url/--sonar-token/--sonar-project-key)",
			strings.Join(missing, ", "),
		)
	}
	if missing := missingGitLabFields(cfg); len(missing) > 0 {
		return Config{}, fmt.Errorf(
			"missing required GitLab configuration: %s (set env vars GITLAB_URL/GITLAB_TOKEN or flags --gitlab-url/--gitlab-token)",
			strings.Join(missing, ", "),
		)
	}
	if missing := missingMergeRequestFields(projectID, mrIID); len(missing) > 0 {
		return Config{}, fmt.Errorf(
			"missing required GitLab merge request context: %s (set env vars CI_PROJECT_ID/CI_MERGE_REQUEST_IID or flags --project-id/--mr-iid)",
			strings.Join(missing, ", "),
		)
	}

	if _, err := url.ParseRequestURI(cfg.SonarURL); err != nil {
		return Config{}, fmt.Errorf("invalid SonarQube URL %q: %w", cfg.SonarURL, err)
	}
	if _, err := url.ParseRequestURI(cfg.GitLabURL); err != nil {
		return Config{}, fmt.Errorf("invalid GitLab URL %q: %w", cfg.GitLabURL, err)
	}

	parsedProjectID, err := strconv.Atoi(projectID)
	if err != nil || parsedProjectID <= 0 {
		return Config{}, fmt.Errorf("invalid project ID %q: expected positive integer", projectID)
	}
	parsedMRIID, err := strconv.Atoi(mrIID)
	if err != nil || parsedMRIID <= 0 {
		return Config{}, fmt.Errorf("invalid merge request IID %q: expected positive integer", mrIID)
	}
	cfg.GitLabProjectID = parsedProjectID
	cfg.GitLabMRIID = parsedMRIID

	if cfg.SeverityThreshold != "" && !sonar.IsValidSeverity(cfg.SeverityThreshold) {
		return Config{}, fmt.Errorf(
			"invalid value for --severity-threshold: %q (allowed: %s)",
			cfg.SeverityThreshold,
			strings.Join(sonar.AllowedSeverities(), ", "),
		)
	}

	return cfg, nil
}

func missingSonarFields(cfg Config) []string {
	var missing []string

	if cfg.SonarURL == "" {
		missing = append(missing, "sonar-url")
	}
	if cfg.SonarToken == "" {
		missing = append(missing, "sonar-token")
	}
	if cfg.SonarProjectKey == "" {
		missing = append(missing, "sonar-project-key")
	}

	return missing
}

func missingGitLabFields(cfg Config) []string {
	var missing []string

	if cfg.GitLabURL == "" {
		missing = append(missing, "gitlab-url")
	}
	if cfg.GitLabToken == "" {
		missing = append(missing, "gitlab-token")
	}

	return missing
}

func missingMergeRequestFields(projectID, mrIID string) []string {
	var missing []string

	if projectID == "" {
		missing = append(missing, "project-id")
	}
	if mrIID == "" {
		missing = append(missing, "mr-iid")
	}

	return missing
}
