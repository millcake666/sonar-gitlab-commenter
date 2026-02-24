package config

import (
	"strings"
	"testing"
)

func TestParseUsesEnvValues(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(nil, mapGetenv(map[string]string{
		"SONAR_HOST_URL":    "https://sonar.example.com",
		"SONAR_TOKEN":       "env-token",
		"SONAR_PROJECT_KEY": "env-project",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.SonarURL != "https://sonar.example.com" {
		t.Fatalf("unexpected URL: %q", cfg.SonarURL)
	}
	if cfg.SonarToken != "env-token" {
		t.Fatalf("unexpected token: %q", cfg.SonarToken)
	}
	if cfg.SonarProjectKey != "env-project" {
		t.Fatalf("unexpected project key: %q", cfg.SonarProjectKey)
	}
}

func TestParseFlagsOverrideEnv(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"--sonar-url=https://flag.example.com",
		"--sonar-token=flag-token",
		"--sonar-project-key=flag-project",
	}, mapGetenv(map[string]string{
		"SONAR_HOST_URL":    "https://env.example.com",
		"SONAR_TOKEN":       "env-token",
		"SONAR_PROJECT_KEY": "env-project",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.SonarURL != "https://flag.example.com" {
		t.Fatalf("unexpected URL: %q", cfg.SonarURL)
	}
	if cfg.SonarToken != "flag-token" {
		t.Fatalf("unexpected token: %q", cfg.SonarToken)
	}
	if cfg.SonarProjectKey != "flag-project" {
		t.Fatalf("unexpected project key: %q", cfg.SonarProjectKey)
	}
}

func TestParseMissingRequiredFields(t *testing.T) {
	t.Parallel()

	_, err := Parse(nil, mapGetenv(map[string]string{}))
	if err == nil {
		t.Fatal("expected error for missing required fields")
	}

	errText := err.Error()
	for _, field := range []string{"sonar-url", "sonar-token", "sonar-project-key"} {
		if !strings.Contains(errText, field) {
			t.Fatalf("error %q does not mention %q", errText, field)
		}
	}
}

func TestParseSeverityThresholdDefaultsToNoFiltering(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(nil, mapGetenv(map[string]string{
		"SONAR_HOST_URL":    "https://sonar.example.com",
		"SONAR_TOKEN":       "env-token",
		"SONAR_PROJECT_KEY": "env-project",
	}))
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

			cfg, err := Parse([]string{"--severity-threshold=" + tc.flagValue}, mapGetenv(map[string]string{
				"SONAR_HOST_URL":    "https://sonar.example.com",
				"SONAR_TOKEN":       "env-token",
				"SONAR_PROJECT_KEY": "env-project",
			}))
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

	_, err := Parse([]string{"--severity-threshold=SEVERE"}, mapGetenv(map[string]string{
		"SONAR_HOST_URL":    "https://sonar.example.com",
		"SONAR_TOKEN":       "env-token",
		"SONAR_PROJECT_KEY": "env-project",
	}))
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

func mapGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
