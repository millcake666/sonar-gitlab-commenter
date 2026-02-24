package sonar

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateAuthenticationSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/authentication/validate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		user, _, ok := r.BasicAuth()
		if !ok || user != "secret-token" {
			t.Fatalf("expected basic auth with token, got user=%q ok=%v", user, ok)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":true}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	if err := client.ValidateAuthentication(context.Background()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateAuthenticationRejected(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":false}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	err := client.ValidateAuthentication(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestFetchProjectIssuesPaginationAndBinding(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		page := r.URL.Query().Get("p")

		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			_, _ = w.Write([]byte(`{
				"issues":[
					{"key":"A","rule":"rule:a","severity":"MAJOR","message":"Issue A","component":"demo:src/a.go","line":10},
					{"key":"B","rule":"rule:b","severity":"MINOR","message":"Issue B","component":"demo:src/b.go","line":0}
				],
				"paging":{"pageIndex":1,"pageSize":2,"total":3}
			}`))
		case "2":
			_, _ = w.Write([]byte(`{
				"issues":[
					{"key":"C","rule":"rule:c","severity":"CRITICAL","message":"Issue C","component":"demo:src/c.go","line":8}
				],
				"paging":{"pageIndex":2,"pageSize":2,"total":3}
			}`))
		default:
			t.Fatalf("unexpected page query: %q", page)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	issues, err := client.FetchProjectIssues(context.Background(), "demo")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(issues) != 2 {
		t.Fatalf("expected 2 bound issues, got %d", len(issues))
	}
	if issues[0].FilePath != "src/a.go" || issues[0].Line != 10 {
		t.Fatalf("unexpected first issue binding: %+v", issues[0])
	}
	if issues[1].FilePath != "src/c.go" || issues[1].Line != 8 {
		t.Fatalf("unexpected second issue binding: %+v", issues[1])
	}
}

func TestFetchProjectIssuesUnauthorized(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	_, err := client.FetchProjectIssues(context.Background(), "demo")
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}
