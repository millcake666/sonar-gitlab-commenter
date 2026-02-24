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
	projectKey = strings.TrimSpace(projectKey)
	if projectKey == "" {
		return nil, fmt.Errorf("project key cannot be empty")
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
