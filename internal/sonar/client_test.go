package sonar

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
					{"key":"A","rule":"rule:a","type":"BUG","severity":"MAJOR","message":"Issue A","component":"demo:src/a.go","line":10},
					{"key":"B","rule":"rule:b","type":"CODE_SMELL","severity":"MINOR","message":"Issue B","component":"demo:src/b.go","line":0}
				],
				"paging":{"pageIndex":1,"pageSize":2,"total":3}
			}`))
		case "2":
			_, _ = w.Write([]byte(`{
				"issues":[
					{"key":"C","rule":"rule:c","type":"VULNERABILITY","severity":"CRITICAL","message":"Issue C","component":"demo:src/c.go","line":8}
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

	if len(issues) != 3 {
		t.Fatalf("expected 3 issues, got %d", len(issues))
	}
	if issues[0].FilePath != "src/a.go" || issues[0].Line != 10 || issues[0].Type != "BUG" {
		t.Fatalf("unexpected first issue content: %+v", issues[0])
	}
	if issues[1].FilePath != "src/b.go" || issues[1].Line != 0 || issues[1].Type != "CODE_SMELL" {
		t.Fatalf("unexpected second issue content: %+v", issues[1])
	}
	if issues[2].FilePath != "src/c.go" || issues[2].Line != 8 || issues[2].Type != "VULNERABILITY" {
		t.Fatalf("unexpected third issue content: %+v", issues[2])
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

func TestFetchQualityReport(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/qualitygates/project_status":
			if got := r.URL.Query().Get("projectKey"); got != "demo" {
				t.Fatalf("unexpected projectKey query for quality gate: %q", got)
			}

			_, _ = w.Write([]byte(`{"projectStatus":{"status":"OK"}}`))
		case "/api/measures/component":
			if got := r.URL.Query().Get("component"); got != "demo" {
				t.Fatalf("unexpected component query for measures: %q", got)
			}
			if got := r.URL.Query().Get("metricKeys"); got != "coverage,new_coverage" {
				t.Fatalf("unexpected metricKeys query for measures: %q", got)
			}

			_, _ = w.Write([]byte(`{
				"component":{
					"measures":[
						{"metric":"coverage","value":"84.3"},
						{"metric":"new_coverage","value":"78.1"}
					]
				}
			}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	report, err := client.FetchQualityReport(context.Background(), "demo")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if report.QualityGateStatus != "passed" {
		t.Fatalf("expected quality gate passed, got %q", report.QualityGateStatus)
	}
	if report.OverallCoverage != 84.3 {
		t.Fatalf("expected overall coverage 84.3, got %v", report.OverallCoverage)
	}
	if report.NewCodeCoverage != 78.1 {
		t.Fatalf("expected new code coverage 78.1, got %v", report.NewCodeCoverage)
	}
}

func TestFetchQualityReportWarningStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/qualitygates/project_status":
			_, _ = w.Write([]byte(`{"projectStatus":{"status":"WARN"}}`))
		case "/api/measures/component":
			_, _ = w.Write([]byte(`{
				"component":{"measures":[
					{"metric":"coverage","value":"100.0"},
					{"metric":"new_coverage","value":"99.9"}
				]}
			}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	report, err := client.FetchQualityReport(context.Background(), "demo")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if report.QualityGateStatus != "warning" {
		t.Fatalf("expected quality gate warning, got %q", report.QualityGateStatus)
	}
}

func TestFetchQualityReportEmptyNewCoverage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/qualitygates/project_status":
			_, _ = w.Write([]byte(`{"projectStatus":{"status":"OK"}}`))
		case "/api/measures/component":
			_, _ = w.Write([]byte(`{
				"component":{"measures":[
					{"metric":"coverage","value":"82.7"},
					{"metric":"new_coverage","value":""}
				]}
			}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	report, err := client.FetchQualityReport(context.Background(), "demo")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if report.NewCodeCoverage != 0 {
		t.Fatalf("expected empty new_coverage treated as 0, got %v", report.NewCodeCoverage)
	}
}

func TestFetchQualityReportMissingCoverageMetric(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/qualitygates/project_status":
			_, _ = w.Write([]byte(`{"projectStatus":{"status":"ERROR"}}`))
		case "/api/measures/component":
			_, _ = w.Write([]byte(`{
				"component":{"measures":[
					{"metric":"coverage","value":"12.5"}
				]}
			}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	_, err := client.FetchQualityReport(context.Background(), "demo")
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "missing SonarQube coverage metrics") {
		t.Fatalf("expected missing metrics error, got %v", err)
	}
}

func TestFetchQualityReportUnauthorized(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-token", server.Client())
	_, err := client.FetchQualityReport(context.Background(), "demo")
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}
