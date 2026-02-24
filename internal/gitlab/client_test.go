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
		_, _ = w.Write([]byte(`{"iid":42}`))
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
