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

func mapGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
