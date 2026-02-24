package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateMergeRequestSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/100/merge_requests/42" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "secret-token" {
			t.Fatalf("expected PRIVATE-TOKEN header, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"iid":42,"diff_refs":{"base_sha":"b","start_sha":"s","head_sha":"h"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	if err := client.ValidateMergeRequest(context.Background(), 100, 42); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateMergeRequestUnauthorized(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	err := client.ValidateMergeRequest(context.Background(), 100, 42)
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestValidateMergeRequestHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	err := client.ValidateMergeRequest(context.Background(), 100, 42)
	if err == nil {
		t.Fatal("expected error")
	}

	for _, expected := range []string{"GitLab API request failed", "HTTP 404"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not contain %q", err, expected)
		}
	}
}

func TestValidateMergeRequestRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	client := NewClient("https://gitlab.example.com", "secret-token", nil)

	err := client.ValidateMergeRequest(context.Background(), 0, 42)
	if err == nil || !strings.Contains(err.Error(), "project ID must be positive") {
		t.Fatalf("expected invalid project ID error, got %v", err)
	}

	err = client.ValidateMergeRequest(context.Background(), 100, 0)
	if err == nil || !strings.Contains(err.Error(), "merge request IID must be positive") {
		t.Fatalf("expected invalid merge request IID error, got %v", err)
	}
}

func TestGetMergeRequestReturnsDiffRefs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"iid":42,"diff_refs":{"base_sha":"base","start_sha":"start","head_sha":"head"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	mr, err := client.GetMergeRequest(context.Background(), 100, 42)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if mr.IID != 42 {
		t.Fatalf("unexpected IID: %d", mr.IID)
	}
	if mr.DiffRefs.BaseSHA != "base" || mr.DiffRefs.StartSHA != "start" || mr.DiffRefs.HeadSHA != "head" {
		t.Fatalf("unexpected diff refs: %+v", mr.DiffRefs)
	}
}

func TestCreateInlineDiscussionSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/100/merge_requests/42/discussions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "secret-token" {
			t.Fatalf("expected PRIVATE-TOKEN header, got %q", got)
		}
		if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/x-www-form-urlencoded") {
			t.Fatalf("unexpected Content-Type: %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}

		expected := map[string]string{
			"body":                    "inline body",
			"position[position_type]": "text",
			"position[base_sha]":      "base",
			"position[start_sha]":     "start",
			"position[head_sha]":      "head",
			"position[old_path]":      "src/main.go",
			"position[new_path]":      "src/main.go",
			"position[new_line]":      "15",
		}
		for key, want := range expected {
			if got := r.PostForm.Get(key); got != want {
				t.Fatalf("unexpected %s: got %q want %q", key, got, want)
			}
		}

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	err := client.CreateInlineDiscussion(
		context.Background(),
		100,
		42,
		"inline body",
		"src/main.go",
		15,
		DiffRefs{
			BaseSHA:  "base",
			StartSHA: "start",
			HeadSHA:  "head",
		},
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestCreateInlineDiscussionRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	client := NewClient("https://gitlab.example.com", "secret-token", nil)

	err := client.CreateInlineDiscussion(context.Background(), 100, 42, "body", "a.go", 1, DiffRefs{})
	if err == nil || !strings.Contains(err.Error(), "merge request diff refs are incomplete") {
		t.Fatalf("expected diff refs validation error, got %v", err)
	}
}

func TestCreateInlineDiscussionUnauthorized(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	err := client.CreateInlineDiscussion(
		context.Background(),
		100,
		42,
		"inline body",
		"src/main.go",
		15,
		DiffRefs{
			BaseSHA:  "base",
			StartSHA: "start",
			HeadSHA:  "head",
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestCreateMergeRequestNoteSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/100/merge_requests/42/notes" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		if got := r.PostForm.Get("body"); got != "summary body" {
			t.Fatalf("unexpected note body: %q", got)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	if err := client.CreateMergeRequestNote(context.Background(), 100, 42, "summary body"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestListMergeRequestDiscussionsWithPagination(t *testing.T) {
	t.Parallel()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/100/merge_requests/42/discussions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Fatalf("unexpected per_page: %q", got)
		}

		switch page := r.URL.Query().Get("page"); page {
		case "1":
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte(`[{"id":"d1","resolved":false,"resolvable":true,"notes":[{"body":"first"}]}]`))
		case "2":
			_, _ = w.Write([]byte(`[{"id":"d2","resolved":true,"resolvable":false,"notes":[{"body":"second"}]}]`))
		default:
			t.Fatalf("unexpected page: %s", page)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	discussions, err := client.ListMergeRequestDiscussions(context.Background(), 100, 42)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("expected 2 requests, got %d", requestCount)
	}
	if len(discussions) != 2 {
		t.Fatalf("expected 2 discussions, got %d", len(discussions))
	}
	if discussions[0].ID != "d1" || discussions[1].ID != "d2" {
		t.Fatalf("unexpected discussions: %+v", discussions)
	}
}

func TestResolveMergeRequestDiscussionSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/100/merge_requests/42/discussions/discussion-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		if got := r.PostForm.Get("resolved"); got != "true" {
			t.Fatalf("unexpected resolved value: %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	if err := client.ResolveMergeRequestDiscussion(context.Background(), 100, 42, "discussion-1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestListMergeRequestNotesWithPagination(t *testing.T) {
	t.Parallel()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/100/merge_requests/42/notes" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Fatalf("unexpected per_page: %q", got)
		}

		switch page := r.URL.Query().Get("page"); page {
		case "1":
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte(`[{"id":11,"body":"note 1"}]`))
		case "2":
			_, _ = w.Write([]byte(`[{"id":12,"body":"note 2"}]`))
		default:
			t.Fatalf("unexpected page: %s", page)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	notes, err := client.ListMergeRequestNotes(context.Background(), 100, 42)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("expected 2 requests, got %d", requestCount)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}
	if notes[0].ID != 11 || notes[1].ID != 12 {
		t.Fatalf("unexpected notes: %+v", notes)
	}
}

func TestUpdateMergeRequestNoteSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/100/merge_requests/42/notes/55" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		if got := r.PostForm.Get("body"); got != "updated summary" {
			t.Fatalf("unexpected note body: %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	if err := client.UpdateMergeRequestNote(context.Background(), 100, 42, 55, "updated summary"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
