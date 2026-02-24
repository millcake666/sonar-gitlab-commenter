package gitlab

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
const perPageLimit = 100

var ErrUnauthorized = errors.New("unauthorized GitLab API request")

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type DiffRefs struct {
	BaseSHA  string
	StartSHA string
	HeadSHA  string
}

type MergeRequest struct {
	IID      int
	DiffRefs DiffRefs
}

type Discussion struct {
	ID         string
	Resolved   bool
	Resolvable bool
	Notes      []DiscussionNote
}

type DiscussionNote struct {
	Body string
}

type MergeRequestNote struct {
	ID   int
	Body string
}

type mergeRequestResponse struct {
	IID      int                  `json:"iid"`
	DiffRefs mergeRequestDiffRefs `json:"diff_refs"`
}

type mergeRequestDiffRefs struct {
	BaseSHA  string `json:"base_sha"`
	StartSHA string `json:"start_sha"`
	HeadSHA  string `json:"head_sha"`
}

type discussionResponse struct {
	ID         string                   `json:"id"`
	Resolved   bool                     `json:"resolved"`
	Resolvable bool                     `json:"resolvable"`
	Notes      []discussionNoteResponse `json:"notes"`
}

type discussionNoteResponse struct {
	Body string `json:"body"`
}

type mergeRequestNoteResponse struct {
	ID   int    `json:"id"`
	Body string `json:"body"`
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
	_, err := c.GetMergeRequest(ctx, projectID, mrIID)
	return err
}

func (c *Client) GetMergeRequest(ctx context.Context, projectID, mrIID int) (MergeRequest, error) {
	if err := validateMergeRequestCoordinates(projectID, mrIID); err != nil {
		return MergeRequest{}, err
	}

	endpoint := fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d", projectID, mrIID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+endpoint, nil)
	if err != nil {
		return MergeRequest{}, fmt.Errorf("failed to create GitLab request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return MergeRequest{}, fmt.Errorf("failed to connect to GitLab at %s: %w", c.baseURL, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return MergeRequest{}, fmt.Errorf("%w: HTTP %d from %s", ErrUnauthorized, resp.StatusCode, endpoint)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyForError))
		return MergeRequest{}, fmt.Errorf("GitLab API request failed for %s: HTTP %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload mergeRequestResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return MergeRequest{}, fmt.Errorf("failed to decode GitLab response from %s: %w", endpoint, err)
	}

	if payload.IID != mrIID {
		return MergeRequest{}, fmt.Errorf("GitLab API returned unexpected merge request IID %d for %s", payload.IID, endpoint)
	}

	return MergeRequest{
		IID: payload.IID,
		DiffRefs: DiffRefs{
			BaseSHA:  payload.DiffRefs.BaseSHA,
			StartSHA: payload.DiffRefs.StartSHA,
			HeadSHA:  payload.DiffRefs.HeadSHA,
		},
	}, nil
}

func (c *Client) CreateInlineDiscussion(
	ctx context.Context,
	projectID,
	mrIID int,
	body,
	path string,
	line int,
	diffRefs DiffRefs,
) error {
	if err := validateMergeRequestCoordinates(projectID, mrIID); err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("discussion body cannot be empty")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("discussion path cannot be empty")
	}
	if line <= 0 {
		return fmt.Errorf("discussion line must be positive")
	}

	normalizedDiffRefs := normalizeDiffRefs(diffRefs)
	if err := validateDiffRefs(normalizedDiffRefs); err != nil {
		return err
	}

	endpoint := fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d/discussions", projectID, mrIID)
	form := url.Values{}
	form.Set("body", body)
	form.Set("position[position_type]", "text")
	form.Set("position[base_sha]", normalizedDiffRefs.BaseSHA)
	form.Set("position[start_sha]", normalizedDiffRefs.StartSHA)
	form.Set("position[head_sha]", normalizedDiffRefs.HeadSHA)
	form.Set("position[old_path]", path)
	form.Set("position[new_path]", path)
	form.Set("position[new_line]", strconv.Itoa(line))

	return c.postForm(ctx, endpoint, form)
}

func (c *Client) CreateMergeRequestNote(ctx context.Context, projectID, mrIID int, body string) error {
	if err := validateMergeRequestCoordinates(projectID, mrIID); err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("note body cannot be empty")
	}

	endpoint := fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d/notes", projectID, mrIID)
	form := url.Values{}
	form.Set("body", body)

	return c.postForm(ctx, endpoint, form)
}

func (c *Client) ListMergeRequestDiscussions(ctx context.Context, projectID, mrIID int) ([]Discussion, error) {
	if err := validateMergeRequestCoordinates(projectID, mrIID); err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d/discussions", projectID, mrIID)
	page := "1"
	discussions := make([]Discussion, 0)

	for {
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			c.baseURL+withPagination(endpoint, page),
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create GitLab request: %w", err)
		}
		req.Header.Set("PRIVATE-TOKEN", c.token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to GitLab at %s: %w", c.baseURL, err)
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("%w: HTTP %d from %s", ErrUnauthorized, resp.StatusCode, endpoint)
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyForError))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("GitLab API request failed for %s: HTTP %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var payload []discussionResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("failed to decode GitLab response from %s: %w", endpoint, err)
		}

		nextPage := strings.TrimSpace(resp.Header.Get("X-Next-Page"))
		_ = resp.Body.Close()

		for _, item := range payload {
			notes := make([]DiscussionNote, 0, len(item.Notes))
			for _, note := range item.Notes {
				notes = append(notes, DiscussionNote(note))
			}
			discussions = append(discussions, Discussion{
				ID:         item.ID,
				Resolved:   item.Resolved,
				Resolvable: item.Resolvable,
				Notes:      notes,
			})
		}

		if nextPage == "" {
			break
		}
		page = nextPage
	}

	return discussions, nil
}

func (c *Client) ResolveMergeRequestDiscussion(ctx context.Context, projectID, mrIID int, discussionID string) error {
	if err := validateMergeRequestCoordinates(projectID, mrIID); err != nil {
		return err
	}
	discussionID = strings.TrimSpace(discussionID)
	if discussionID == "" {
		return fmt.Errorf("discussion ID cannot be empty")
	}

	endpoint := fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d/discussions/%s", projectID, mrIID, discussionID)
	form := url.Values{}
	form.Set("resolved", "true")

	return c.putForm(ctx, endpoint, form)
}

func (c *Client) ListMergeRequestNotes(ctx context.Context, projectID, mrIID int) ([]MergeRequestNote, error) {
	if err := validateMergeRequestCoordinates(projectID, mrIID); err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d/notes", projectID, mrIID)
	page := "1"
	notes := make([]MergeRequestNote, 0)

	for {
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			c.baseURL+withPagination(endpoint, page),
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create GitLab request: %w", err)
		}
		req.Header.Set("PRIVATE-TOKEN", c.token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to GitLab at %s: %w", c.baseURL, err)
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("%w: HTTP %d from %s", ErrUnauthorized, resp.StatusCode, endpoint)
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyForError))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("GitLab API request failed for %s: HTTP %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var payload []mergeRequestNoteResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("failed to decode GitLab response from %s: %w", endpoint, err)
		}

		nextPage := strings.TrimSpace(resp.Header.Get("X-Next-Page"))
		_ = resp.Body.Close()

		for _, item := range payload {
			notes = append(notes, MergeRequestNote(item))
		}

		if nextPage == "" {
			break
		}
		page = nextPage
	}

	return notes, nil
}

func (c *Client) UpdateMergeRequestNote(ctx context.Context, projectID, mrIID, noteID int, body string) error {
	if err := validateMergeRequestCoordinates(projectID, mrIID); err != nil {
		return err
	}
	if noteID <= 0 {
		return fmt.Errorf("note ID must be positive")
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("note body cannot be empty")
	}

	endpoint := fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d/notes/%d", projectID, mrIID, noteID)
	form := url.Values{}
	form.Set("body", body)

	return c.putForm(ctx, endpoint, form)
}

func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values) error {
	return c.sendForm(ctx, http.MethodPost, endpoint, form)
}

func (c *Client) putForm(ctx context.Context, endpoint string, form url.Values) error {
	return c.sendForm(ctx, http.MethodPut, endpoint, form)
}

func (c *Client) sendForm(ctx context.Context, method, endpoint string, form url.Values) error {
	req, err := http.NewRequestWithContext(
		ctx,
		method,
		c.baseURL+endpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("failed to create GitLab request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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

	return nil
}

func withPagination(endpoint, page string) string {
	page = strings.TrimSpace(page)
	if page == "" {
		page = "1"
	}

	values := url.Values{}
	values.Set("per_page", strconv.Itoa(perPageLimit))
	values.Set("page", page)

	return endpoint + "?" + values.Encode()
}

func validateMergeRequestCoordinates(projectID, mrIID int) error {
	if projectID <= 0 {
		return fmt.Errorf("project ID must be positive")
	}
	if mrIID <= 0 {
		return fmt.Errorf("merge request IID must be positive")
	}

	return nil
}

func validateDiffRefs(diffRefs DiffRefs) error {
	if diffRefs.BaseSHA == "" || diffRefs.StartSHA == "" || diffRefs.HeadSHA == "" {
		return fmt.Errorf(
			"merge request diff refs are incomplete: base_sha=%t start_sha=%t head_sha=%t",
			diffRefs.BaseSHA != "",
			diffRefs.StartSHA != "",
			diffRefs.HeadSHA != "",
		)
	}

	return nil
}

func normalizeDiffRefs(diffRefs DiffRefs) DiffRefs {
	return DiffRefs{
		BaseSHA:  strings.TrimSpace(diffRefs.BaseSHA),
		StartSHA: strings.TrimSpace(diffRefs.StartSHA),
		HeadSHA:  strings.TrimSpace(diffRefs.HeadSHA),
	}
}
