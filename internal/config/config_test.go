package config

import (
	"errors"
	"strings"
	"testing"
)

func TestParseUsesEnvValues(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(nil, mapGetenv(baseEnv()))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.SonarURL != "https://sonar.example.com" {
		t.Fatalf("unexpected Sonar URL: %q", cfg.SonarURL)
	}
	if cfg.SonarToken != "env-sonar-token" {
		t.Fatalf("unexpected Sonar token: %q", cfg.SonarToken)
	}
	if cfg.SonarProjectKey != "env-project" {
		t.Fatalf("unexpected Sonar project key: %q", cfg.SonarProjectKey)
	}
	if cfg.GitLabURL != "https://gitlab.example.com" {
		t.Fatalf("unexpected GitLab URL: %q", cfg.GitLabURL)
	}
	if cfg.GitLabToken != "env-gitlab-token" {
		t.Fatalf("unexpected GitLab token: %q", cfg.GitLabToken)
	}
	if cfg.GitLabProjectID != 100 {
		t.Fatalf("unexpected GitLab project ID: %d", cfg.GitLabProjectID)
	}
	if cfg.GitLabMRIID != 42 {
		t.Fatalf("unexpected GitLab MR IID: %d", cfg.GitLabMRIID)
	}
}

func TestParseFlagsOverrideEnv(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"--sonar-url=https://sonar-flag.example.com",
		"--sonar-token=flag-sonar-token",
		"--sonar-project-key=flag-project",
		"--gitlab-url=https://gitlab-flag.example.com",
		"--gitlab-token=flag-gitlab-token",
		"--project-id=200",
		"--mr-iid=7",
	}, mapGetenv(baseEnv()))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.SonarURL != "https://sonar-flag.example.com" {
		t.Fatalf("unexpected Sonar URL: %q", cfg.SonarURL)
	}
	if cfg.SonarToken != "flag-sonar-token" {
		t.Fatalf("unexpected Sonar token: %q", cfg.SonarToken)
	}
	if cfg.SonarProjectKey != "flag-project" {
		t.Fatalf("unexpected Sonar project key: %q", cfg.SonarProjectKey)
	}
	if cfg.GitLabURL != "https://gitlab-flag.example.com" {
		t.Fatalf("unexpected GitLab URL: %q", cfg.GitLabURL)
	}
	if cfg.GitLabToken != "flag-gitlab-token" {
		t.Fatalf("unexpected GitLab token: %q", cfg.GitLabToken)
	}
	if cfg.GitLabProjectID != 200 {
		t.Fatalf("unexpected GitLab project ID: %d", cfg.GitLabProjectID)
	}
	if cfg.GitLabMRIID != 7 {
		t.Fatalf("unexpected GitLab MR IID: %d", cfg.GitLabMRIID)
	}
}

func TestParseMissingRequiredSonarFields(t *testing.T) {
	t.Parallel()

	env := baseEnv()
	delete(env, "SONAR_HOST_URL")
	delete(env, "SONAR_TOKEN")
	delete(env, "SONAR_PROJECT_KEY")

	_, err := Parse(nil, mapGetenv(env))
	if err == nil {
		t.Fatal("expected error for missing required SonarQube fields")
	}

	errText := err.Error()
	for _, field := range []string{"sonar-url", "sonar-token", "sonar-project-key"} {
		if !strings.Contains(errText, field) {
			t.Fatalf("error %q does not mention %q", errText, field)
		}
	}
}

func TestParseMissingRequiredGitLabFields(t *testing.T) {
	t.Parallel()

	env := baseEnv()
	delete(env, "GITLAB_URL")
	delete(env, "GITLAB_TOKEN")

	_, err := Parse(nil, mapGetenv(env))
	if err == nil {
		t.Fatal("expected error for missing required GitLab fields")
	}

	errText := err.Error()
	for _, field := range []string{"gitlab-url", "gitlab-token"} {
		if !strings.Contains(errText, field) {
			t.Fatalf("error %q does not mention %q", errText, field)
		}
	}
}

func TestParseMissingMergeRequestContext(t *testing.T) {
	t.Parallel()

	env := baseEnv()
	delete(env, "CI_PROJECT_ID")
	delete(env, "CI_MERGE_REQUEST_IID")

	_, err := Parse(nil, mapGetenv(env))
	if err == nil {
		t.Fatal("expected error for missing MR context")
	}

	errText := err.Error()
	for _, field := range []string{"project-id", "mr-iid"} {
		if !strings.Contains(errText, field) {
			t.Fatalf("error %q does not mention %q", errText, field)
		}
	}
}

func TestParseRejectsInvalidProjectID(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"--project-id=abc"}, mapGetenv(baseEnv()))
	if err == nil {
		t.Fatal("expected error for invalid project ID")
	}

	errText := err.Error()
	for _, expected := range []string{"invalid project ID", "abc"} {
		if !strings.Contains(errText, expected) {
			t.Fatalf("error %q does not contain %q", errText, expected)
		}
	}
}

func TestParseRejectsInvalidMRIID(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"--mr-iid=0"}, mapGetenv(baseEnv()))
	if err == nil {
		t.Fatal("expected error for invalid MR IID")
	}

	errText := err.Error()
	for _, expected := range []string{"invalid merge request IID", "0"} {
		if !strings.Contains(errText, expected) {
			t.Fatalf("error %q does not contain %q", errText, expected)
		}
	}
}

func TestParseSeverityThresholdDefaultsToNoFiltering(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(nil, mapGetenv(baseEnv()))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.SeverityThreshold != "" {
		t.Fatalf("expected empty severity threshold by default, got %q", cfg.SeverityThreshold)
	}
}

func TestParseSeverityThresholdAcceptsSupportedValues(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		flagValue string
		expected  string
	}{
		{flagValue: "INFO", expected: "INFO"},
		{flagValue: "minor", expected: "MINOR"},
		{flagValue: "MAJOR", expected: "MAJOR"},
		{flagValue: "critical", expected: "CRITICAL"},
		{flagValue: "BLOCKER", expected: "BLOCKER"},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.flagValue, func(t *testing.T) {
			t.Parallel()

			cfg, err := Parse([]string{"--severity-threshold=" + tc.flagValue}, mapGetenv(baseEnv()))
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if cfg.SeverityThreshold != tc.expected {
				t.Fatalf("unexpected severity threshold: got %q want %q", cfg.SeverityThreshold, tc.expected)
			}
		})
	}
}

func TestParseSeverityThresholdRejectsUnsupportedValue(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"--severity-threshold=SEVERE"}, mapGetenv(baseEnv()))
	if err == nil {
		t.Fatal("expected error for unsupported severity threshold")
	}

	errText := err.Error()
	for _, expected := range []string{
		"invalid value for --severity-threshold",
		"SEVERE",
		"INFO, MINOR, MAJOR, CRITICAL, BLOCKER",
	} {
		if !strings.Contains(errText, expected) {
			t.Fatalf("error %q does not contain %q", errText, expected)
		}
	}
}

func TestParseDryRunFlag(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{"--dry-run"}, mapGetenv(baseEnv()))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !cfg.DryRun {
		t.Fatal("expected dry-run mode to be enabled")
	}
}

func TestParseLogsFlag(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{"--logs=true"}, mapGetenv(baseEnv()))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !cfg.Logs {
		t.Fatal("expected logs mode to be enabled")
	}
}

func TestParseHelpReturnsDocumentation(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"--help"}, mapGetenv(baseEnv()))
	if err == nil {
		t.Fatal("expected help error")
	}

	var helpErr *HelpError
	if !errors.As(err, &helpErr) {
		t.Fatalf("expected HelpError, got %v", err)
	}

	for _, expected := range []string{
		"--sonar-url",
		"--dry-run",
		"--logs",
		"--severity-threshold",
		"--gitlab-url",
		"--project-id",
		"SONAR_HOST_URL",
		"SONAR_TOKEN",
		"SONAR_PROJECT_KEY",
		"GITLAB_URL",
		"GITLAB_TOKEN",
		"CI_PROJECT_ID",
		"CI_MERGE_REQUEST_IID",
	} {
		if !strings.Contains(helpErr.Message, expected) {
			t.Fatalf("help output %q does not contain %q", helpErr.Message, expected)
		}
	}
}

func baseEnv() map[string]string {
	return map[string]string{
		"SONAR_HOST_URL":       "https://sonar.example.com",
		"SONAR_TOKEN":          "env-sonar-token",
		"SONAR_PROJECT_KEY":    "env-project",
		"GITLAB_URL":           "https://gitlab.example.com",
		"GITLAB_TOKEN":         "env-gitlab-token",
		"CI_PROJECT_ID":        "100",
		"CI_MERGE_REQUEST_IID": "42",
	}
}

func mapGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
