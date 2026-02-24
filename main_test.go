package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sonar-gitlab-commenter/internal/gitlab"
	"sonar-gitlab-commenter/internal/sonar"
)

func TestFormatMergeRequestSummaryComment(t *testing.T) {
	t.Parallel()

	issues := []sonar.Issue{
		{Key: "A", Severity: "major"},
		{Key: "B", Severity: "CRITICAL"},
		{Key: "C", Severity: "BLOCKER"},
		{Key: "D", Severity: "unknown"},
	}
	projectLevelIssues := []sonar.Issue{
		{
			Severity: "MAJOR",
			Type:     "CODE_SMELL",
			Message:  "Project level issue",
			Rule:     "go:S100",
		},
	}

	comment := formatMergeRequestSummaryComment(
		sonar.QualityReport{
			QualityGateStatus: "passed",
			OverallCoverage:   82.4,
			NewCodeCoverage:   75.1,
		},
		issues,
		projectLevelIssues,
	)

	assertCommentContains(t, comment, commentMarker)
	assertCommentContains(t, comment, "Quality gate: ✅ **passed**")
	assertCommentContains(t, comment, "Overall coverage: 82.40%")
	assertCommentContains(t, comment, "New code coverage: 75.10%")
	assertCommentContains(t, comment, "Total issues: 4")
	assertCommentContains(t, comment, "BLOCKER: 1")
	assertCommentContains(t, comment, "CRITICAL: 1")
	assertCommentContains(t, comment, "MAJOR: 1")
	assertCommentContains(t, comment, "MINOR: 0")
	assertCommentContains(t, comment, "INFO: 0")
	assertCommentContains(t, comment, "UNKNOWN: 1")
	assertCommentContains(t, comment, "**SonarQube issues without line binding**")
	assertCommentContains(t, comment, "1. [MAJOR][CODE_SMELL] Project level issue (rule `go:S100`)")
}

func TestFormatMergeRequestSummaryCommentWithoutProjectLevelIssues(t *testing.T) {
	t.Parallel()

	comment := formatMergeRequestSummaryComment(
		sonar.QualityReport{QualityGateStatus: "failed"},
		[]sonar.Issue{{Severity: "MINOR"}},
		nil,
	)

	assertCommentContains(t, comment, "Quality gate: ❌ **failed**")
	assertCommentContains(t, comment, "MINOR: 1")
	if strings.Contains(comment, "without line binding") {
		t.Fatalf("did not expect project-level section, got %q", comment)
	}
}

func TestFindLatestSummaryNote(t *testing.T) {
	t.Parallel()

	notes := []gitlab.MergeRequestNote{
		{ID: 11, Body: "regular note"},
		{ID: 20, Body: commentMarker + "\n**SonarQube issue**"},
		{ID: 30, Body: commentMarker + "\n" + summaryHeading},
		{ID: 31, Body: commentMarker + "\n" + summaryHeading + "\nupdated"},
	}

	note, found := findLatestSummaryNote(notes)
	if !found {
		t.Fatal("expected summary note to be found")
	}
	if note.ID != 31 {
		t.Fatalf("unexpected note selected: %+v", note)
	}
}

func TestFindLatestSummaryNoteWhenMissing(t *testing.T) {
	t.Parallel()

	_, found := findLatestSummaryNote([]gitlab.MergeRequestNote{
		{ID: 1, Body: "plain note"},
		{ID: 2, Body: commentMarker + "\n**SonarQube issue**"},
	})
	if found {
		t.Fatal("did not expect summary note")
	}
}

func TestDiscussionContainsMarker(t *testing.T) {
	t.Parallel()

	if !discussionContainsMarker(gitlab.Discussion{
		Notes: []gitlab.DiscussionNote{{Body: "hello"}, {Body: commentMarker + " tool"}},
	}) {
		t.Fatal("expected marker discussion to match")
	}

	if discussionContainsMarker(gitlab.Discussion{
		Notes: []gitlab.DiscussionNote{{Body: "hello"}, {Body: "world"}},
	}) {
		t.Fatal("did not expect discussion without marker to match")
	}
}

func TestResolvePreviousSonarDiscussionsResolvesOnlyToolThreads(t *testing.T) {
	t.Parallel()

	resolvedCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/100/merge_requests/42/discussions":
			_, _ = w.Write([]byte(`[
				{"id":"tool-open","resolved":false,"resolvable":true,"notes":[{"body":"` + commentMarker + `\nold"}]},
				{"id":"tool-resolved","resolved":true,"resolvable":true,"notes":[{"body":"` + commentMarker + `\nresolved"}]},
				{"id":"other-open","resolved":false,"resolvable":true,"notes":[{"body":"external"}]}
			]`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v4/projects/100/merge_requests/42/discussions/tool-open":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("failed to parse form: %v", err)
			}
			if got := r.PostForm.Get("resolved"); got != "true" {
				t.Fatalf("unexpected resolved value: %q", got)
			}
			resolvedCalls++
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := gitlab.NewClient(server.URL, "secret-token", server.Client())
	resolvedCount, err := resolvePreviousSonarDiscussions(context.Background(), client, 100, 42)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resolvedCount != 1 {
		t.Fatalf("expected 1 resolved discussion, got %d", resolvedCount)
	}
	if resolvedCalls != 1 {
		t.Fatalf("expected 1 resolve request, got %d", resolvedCalls)
	}
}

func TestUpsertSummaryNoteCreatesOnFirstRun(t *testing.T) {
	t.Parallel()

	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/100/merge_requests/42/notes":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects/100/merge_requests/42/notes":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("failed to parse form: %v", err)
			}
			body := r.PostForm.Get("body")
			if !strings.Contains(body, commentMarker) || !strings.Contains(body, summaryHeading) {
				t.Fatalf("unexpected created body: %q", body)
			}
			createCalls++
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := gitlab.NewClient(server.URL, "secret-token", server.Client())
	updated, err := upsertSummaryNote(context.Background(), client, 100, 42, commentMarker+"\n"+summaryHeading+"\nnew")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if updated {
		t.Fatal("expected create path, got update")
	}
	if createCalls != 1 {
		t.Fatalf("expected one create call, got %d", createCalls)
	}
}

func TestUpsertSummaryNoteUpdatesExistingSummary(t *testing.T) {
	t.Parallel()

	updateCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/100/merge_requests/42/notes":
			_, _ = w.Write([]byte(`[
				{"id":10,"body":"plain note"},
				{"id":11,"body":"` + commentMarker + `\n` + summaryHeading + `\nold"}
			]`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v4/projects/100/merge_requests/42/notes/11":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("failed to parse form: %v", err)
			}
			if got := r.PostForm.Get("body"); !strings.Contains(got, "fresh summary") {
				t.Fatalf("unexpected update body: %q", got)
			}
			updateCalls++
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := gitlab.NewClient(server.URL, "secret-token", server.Client())
	updated, err := upsertSummaryNote(context.Background(), client, 100, 42, commentMarker+"\n"+summaryHeading+"\nfresh summary")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !updated {
		t.Fatal("expected update path")
	}
	if updateCalls != 1 {
		t.Fatalf("expected one update call, got %d", updateCalls)
	}
}

func assertCommentContains(t *testing.T, comment, expected string) {
	t.Helper()

	if !strings.Contains(comment, expected) {
		t.Fatalf("comment %q does not contain %q", comment, expected)
	}
}
