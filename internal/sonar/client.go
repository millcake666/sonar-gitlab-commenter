package sonar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBodyForError = 512

var ErrUnauthorized = errors.New("unauthorized SonarQube API request")

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type Issue struct {
	Key      string
	Rule     string
	Severity string
	Message  string
	FilePath string
	Line     int
}

type QualityReport struct {
	QualityGateStatus string
	OverallCoverage   float64
	NewCodeCoverage   float64
}

const (
	qualityGatePassed  = "passed"
	qualityGateFailed  = "failed"
	qualityGateWarning = "warning"
)

type authenticationResponse struct {
	Valid bool `json:"valid"`
}

type issuesSearchResponse struct {
	Issues []apiIssue `json:"issues"`
	Paging struct {
		PageIndex int `json:"pageIndex"`
		PageSize  int `json:"pageSize"`
		Total     int `json:"total"`
	} `json:"paging"`
}

type apiIssue struct {
	Key       string `json:"key"`
	Rule      string `json:"rule"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Component string `json:"component"`
	Line      int    `json:"line"`
}

type qualityGateProjectStatusResponse struct {
	ProjectStatus struct {
		Status string `json:"status"`
	} `json:"projectStatus"`
}

type measuresComponentResponse struct {
	Component struct {
		Measures []apiMeasure `json:"measures"`
	} `json:"component"`
}

type apiMeasure struct {
	Metric string `json:"metric"`
	Value  string `json:"value"`
}

func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	normalizedURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	return &Client{
		baseURL:    normalizedURL,
		token:      strings.TrimSpace(token),
		httpClient: httpClient,
	}
}

func (c *Client) ValidateAuthentication(ctx context.Context) error {
	values := url.Values{}
	var payload authenticationResponse

	if err := c.getJSON(ctx, "/api/authentication/validate", values, &payload); err != nil {
		return err
	}

	if !payload.Valid {
		return fmt.Errorf("%w: SonarQube token rejected by /api/authentication/validate", ErrUnauthorized)
	}

	return nil
}

func (c *Client) FetchProjectIssues(ctx context.Context, projectKey string) ([]Issue, error) {
	projectKey, err := normalizeProjectKey(projectKey)
	if err != nil {
		return nil, err
	}

	const pageSize = 500
	var (
		allIssues []Issue
		page      = 1
	)

	for {
		values := url.Values{}
		values.Set("componentKeys", projectKey)
		values.Set("p", strconv.Itoa(page))
		values.Set("ps", strconv.Itoa(pageSize))

		var payload issuesSearchResponse
		if err := c.getJSON(ctx, "/api/issues/search", values, &payload); err != nil {
			return nil, err
		}

		for _, issue := range payload.Issues {
			filePath := extractFilePath(issue.Component)
			if filePath == "" || issue.Line <= 0 {
				continue
			}

			allIssues = append(allIssues, Issue{
				Key:      issue.Key,
				Rule:     issue.Rule,
				Severity: issue.Severity,
				Message:  issue.Message,
				FilePath: filePath,
				Line:     issue.Line,
			})
		}

		if payload.Paging.PageSize <= 0 || page*payload.Paging.PageSize >= payload.Paging.Total {
			break
		}

		page++
	}

	return allIssues, nil
}

func (c *Client) FetchQualityReport(ctx context.Context, projectKey string) (QualityReport, error) {
	projectKey, err := normalizeProjectKey(projectKey)
	if err != nil {
		return QualityReport{}, err
	}

	qualityGateStatus, err := c.fetchQualityGateStatus(ctx, projectKey)
	if err != nil {
		return QualityReport{}, err
	}

	overallCoverage, newCodeCoverage, err := c.fetchCoverageMetrics(ctx, projectKey)
	if err != nil {
		return QualityReport{}, err
	}

	return QualityReport{
		QualityGateStatus: qualityGateStatus,
		OverallCoverage:   overallCoverage,
		NewCodeCoverage:   newCodeCoverage,
	}, nil
}

func (c *Client) fetchQualityGateStatus(ctx context.Context, projectKey string) (string, error) {
	values := url.Values{}
	values.Set("projectKey", projectKey)

	var payload qualityGateProjectStatusResponse
	if err := c.getJSON(ctx, "/api/qualitygates/project_status", values, &payload); err != nil {
		return "", err
	}

	return mapQualityGateStatus(payload.ProjectStatus.Status), nil
}

func (c *Client) fetchCoverageMetrics(ctx context.Context, projectKey string) (float64, float64, error) {
	values := url.Values{}
	values.Set("component", projectKey)
	values.Set("metricKeys", "coverage,new_coverage")

	var payload measuresComponentResponse
	if err := c.getJSON(ctx, "/api/measures/component", values, &payload); err != nil {
		return 0, 0, err
	}

	var (
		overallCoverage float64
		newCoverage     float64
		overallFound    bool
		newFound        bool
	)

	for _, measure := range payload.Component.Measures {
		switch measure.Metric {
		case "coverage":
			parsed, err := strconv.ParseFloat(measure.Value, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to parse SonarQube metric coverage value %q: %w", measure.Value, err)
			}

			overallCoverage = parsed
			overallFound = true
		case "new_coverage":
			parsed, err := strconv.ParseFloat(measure.Value, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to parse SonarQube metric new_coverage value %q: %w", measure.Value, err)
			}

			newCoverage = parsed
			newFound = true
		}
	}

	if !overallFound || !newFound {
		return 0, 0, fmt.Errorf("missing SonarQube coverage metrics: coverage=%t new_coverage=%t", overallFound, newFound)
	}

	return overallCoverage, newCoverage, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, query url.Values, target any) error {
	requestURL := c.baseURL + endpoint
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create SonarQube request: %w", err)
	}

	req.SetBasicAuth(c.token, "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to SonarQube at %s: %w", c.baseURL, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: HTTP %d from %s", ErrUnauthorized, resp.StatusCode, endpoint)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyForError))
		return fmt.Errorf("SonarQube API request failed for %s: HTTP %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("failed to decode SonarQube response from %s: %w", endpoint, err)
	}

	return nil
}

func extractFilePath(component string) string {
	component = strings.TrimSpace(component)
	if component == "" {
		return ""
	}

	if idx := strings.Index(component, ":"); idx >= 0 && idx < len(component)-1 {
		return component[idx+1:]
	}

	return component
}

func normalizeProjectKey(projectKey string) (string, error) {
	projectKey = strings.TrimSpace(projectKey)
	if projectKey == "" {
		return "", fmt.Errorf("project key cannot be empty")
	}

	return projectKey, nil
}

func mapQualityGateStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "OK":
		return qualityGatePassed
	case "ERROR":
		return qualityGateFailed
	case "WARN":
		return qualityGateWarning
	default:
		return qualityGateWarning
	}
}
