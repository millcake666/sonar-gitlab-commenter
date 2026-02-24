package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBodyForError = 512

var ErrUnauthorized = errors.New("unauthorized GitLab API request")

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type mergeRequestResponse struct {
	IID int `json:"iid"`
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

func (c *Client) ValidateMergeRequest(ctx context.Context, projectID, mrIID int) error {
	if projectID <= 0 {
		return fmt.Errorf("project ID must be positive")
	}
	if mrIID <= 0 {
		return fmt.Errorf("merge request IID must be positive")
	}

	endpoint := fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d", projectID, mrIID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create GitLab request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to GitLab at %s: %w", c.baseURL, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: HTTP %d from %s", ErrUnauthorized, resp.StatusCode, endpoint)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyForError))
		return fmt.Errorf("GitLab API request failed for %s: HTTP %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload mergeRequestResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("failed to decode GitLab response from %s: %w", endpoint, err)
	}

	if payload.IID != mrIID {
		return fmt.Errorf("GitLab API returned unexpected merge request IID %d for %s", payload.IID, endpoint)
	}

	return nil
}
