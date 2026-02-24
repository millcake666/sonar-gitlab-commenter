package config

import (
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"
)

type Config struct {
	SonarURL        string
	SonarToken      string
	SonarProjectKey string
}

func Parse(args []string, getenv func(string) string) (Config, error) {
	cfg := Config{
		SonarURL:        strings.TrimSpace(getenv("SONAR_HOST_URL")),
		SonarToken:      strings.TrimSpace(getenv("SONAR_TOKEN")),
		SonarProjectKey: strings.TrimSpace(getenv("SONAR_PROJECT_KEY")),
	}

	fs := flag.NewFlagSet("sonar-gitlab-commenter", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&cfg.SonarURL, "sonar-url", cfg.SonarURL, "SonarQube server URL (env: SONAR_HOST_URL)")
	fs.StringVar(&cfg.SonarToken, "sonar-token", cfg.SonarToken, "SonarQube access token (env: SONAR_TOKEN)")
	fs.StringVar(&cfg.SonarProjectKey, "sonar-project-key", cfg.SonarProjectKey, "SonarQube project key (env: SONAR_PROJECT_KEY)")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("invalid CLI arguments: %w", err)
	}

	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}

	cfg.SonarURL = strings.TrimSpace(cfg.SonarURL)
	cfg.SonarToken = strings.TrimSpace(cfg.SonarToken)
	cfg.SonarProjectKey = strings.TrimSpace(cfg.SonarProjectKey)

	if missing := missingFields(cfg); len(missing) > 0 {
		return Config{}, fmt.Errorf(
			"missing required SonarQube configuration: %s (set env vars SONAR_HOST_URL/SONAR_TOKEN/SONAR_PROJECT_KEY or flags --sonar-url/--sonar-token/--sonar-project-key)",
			strings.Join(missing, ", "),
		)
	}

	if _, err := url.ParseRequestURI(cfg.SonarURL); err != nil {
		return Config{}, fmt.Errorf("invalid SonarQube URL %q: %w", cfg.SonarURL, err)
	}

	return cfg, nil
}

func missingFields(cfg Config) []string {
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
