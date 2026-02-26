package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestExtractDiffLines(t *testing.T) {
	t.Parallel()

	diff := "@@ -5,2 +10,3 @@\n+line 10\n+line 11\n+line 12\n@@ -20,2 +40,2 @@\n-old\n+line 40\n context"
	lines := extractDiffLines(diff)

	expectedLines := map[int]struct {
		lineType lineType
		oldLine  int
		newLine  int
	}{
		10: {lineType: lineTypeAdded, newLine: 10},
		11: {lineType: lineTypeAdded, newLine: 11},
		12: {lineType: lineTypeAdded, newLine: 12},
		40: {lineType: lineTypeAdded, newLine: 40},
		41: {lineType: lineTypeContext, oldLine: 21, newLine: 41},
	}

	if len(lines) != len(expectedLines) {
		t.Fatalf("expected %d visible lines, got %d", len(expectedLines), len(lines))
	}

	for newLine, expected := range expectedLines {
		actual, found := lines[newLine]
		if !found {
			t.Fatalf("expected line %d not found", newLine)
		}
		if actual.lineType != expected.lineType {
			t.Fatalf("line %d: expected type %d, got %d", newLine, expected.lineType, actual.lineType)
		}
		if actual.oldLine != expected.oldLine {
			t.Fatalf("line %d: expected oldLine %d, got %d", newLine, expected.oldLine, actual.oldLine)
		}
		if actual.newLine != expected.newLine {
			t.Fatalf("line %d: expected newLine %d, got %d", newLine, expected.newLine, actual.newLine)
		}
	}
}

func TestBuildDiffLineIndex(t *testing.T) {
	t.Parallel()

	changes := []gitlab.MergeRequestChange{
		{
			OldPath: "src/old.go",
			NewPath: "src/new.go",
			Diff:    "@@ -0,0 +10,2 @@\n+line 10\n+line 11",
		},
		{
			OldPath: "src/main.go",
			NewPath: "src/main.go",
			Diff:    "@@ -0,0 +20,1 @@\n+line 20",
		},
	}

	index := buildDiffLineIndex(changes)

	if len(index.lines) != 2 {
		t.Fatalf("expected 2 files in index, got %d", len(index.lines))
	}
	if len(index.pathMap) != 2 {
		t.Fatalf("expected 2 paths in pathMap, got %d", len(index.pathMap))
	}

	if _, ok := index.lines["src/new.go"][10]; !ok {
		t.Fatal("expected line 10 in src/new.go")
	}
	if _, ok := index.lines["src/new.go"][11]; !ok {
		t.Fatal("expected line 11 in src/new.go")
	}

	pathInfo := index.pathMap["src/new.go"]
	if pathInfo.oldPath != "src/old.go" || pathInfo.newPath != "src/new.go" {
		t.Fatalf("unexpected path info for src/new.go: %+v", pathInfo)
	}
}

func TestFilterIssuesByMRDiff(t *testing.T) {
	t.Parallel()

	index := diffLineIndex{
		lines: map[string]map[int]lineInfo{
			"src/main.go": {
				12: {lineType: lineTypeAdded, newLine: 12},
			},
		},
		pathMap: map[string]pathInfo{
			"src/main.go": {
				oldPath: "src/main.go",
				newPath: "src/main.go",
			},
		},
	}
	issues := []sonar.Issue{
		{Key: "A", FilePath: "src/main.go", Line: 12},
		{Key: "B", FilePath: "src/main.go", Line: 13},
		{Key: "C", FilePath: "src/other.go", Line: 12},
		{Key: "D", FilePath: "src/main.go", Line: 0},
	}

	filtered := filterIssuesByMRDiff(issues, index)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 issue after diff filter, got %d", len(filtered))
	}
	if filtered[0].Key != "A" {
		t.Fatalf("unexpected issue after diff filter: %+v", filtered[0])
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

func TestRunWithHelpReturnsSuccessAndWritesDocumentation(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := runWith(
		[]string{"--help"},
		func(string) string { return "" },
		&output,
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	helpText := output.String()
	for _, expected := range []string{"--sonar-url", "--dry-run", "SONAR_HOST_URL", "CI_PROJECT_ID"} {
		if !strings.Contains(helpText, expected) {
			t.Fatalf("help output %q does not contain %q", helpText, expected)
		}
	}
}

func TestRunWithDryRunSkipsGitLabWriteOperations(t *testing.T) {
	t.Parallel()

	var writeCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/100/merge_requests/42":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"iid":42,"diff_refs":{"base_sha":"base","start_sha":"start","head_sha":"head"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/100/merge_requests/42/changes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"changes":[
					{"old_path":"main.go","new_path":"main.go","diff":"@@ -0,0 +12,1 @@\n+added line"}
				]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/authentication/validate":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"valid":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"issues":[
					{"key":"ISSUE-1","rule":"go:S100","type":"CODE_SMELL","severity":"MAJOR","message":"inline issue","component":"project:main.go","line":12},
					{"key":"ISSUE-2","rule":"go:S200","type":"BUG","severity":"MINOR","message":"project issue","component":"project"}
				],
				"paging":{"pageIndex":1,"pageSize":500,"total":2}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/qualitygates/project_status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"projectStatus":{"status":"OK"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/measures/component":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"component":{"measures":[{"metric":"coverage","value":"80.5"},{"metric":"new_coverage","value":"70.0"}]}}`))
		case r.Method == http.MethodPost || r.Method == http.MethodPut:
			atomic.AddInt32(&writeCalls, 1)
			t.Fatalf("did not expect write request in dry-run mode: %s %s", r.Method, r.URL.Path)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var output bytes.Buffer
	err := runWith(
		[]string{
			"--sonar-url=" + server.URL,
			"--sonar-token=token",
			"--sonar-project-key=project",
			"--gitlab-url=" + server.URL,
			"--gitlab-token=token",
			"--project-id=100",
			"--mr-iid=42",
			"--dry-run",
		},
		func(string) string { return "" },
		&output,
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got := atomic.LoadInt32(&writeCalls); got != 0 {
		t.Fatalf("expected no GitLab write calls, got %d", got)
	}

	logOutput := output.String()
	for _, expected := range []string{
		"Dry-run enabled",
		"Action log: found 1 issues, published 0 comments",
	} {
		if !strings.Contains(logOutput, expected) {
			t.Fatalf("output %q does not contain %q", logOutput, expected)
		}
	}
}

func TestRunWithLogsFlagPrintsFetchedSonarIssues(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/100/merge_requests/42":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"iid":42,"diff_refs":{"base_sha":"base","start_sha":"start","head_sha":"head"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/100/merge_requests/42/changes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"changes":[
					{"old_path":"main.go","new_path":"main.go","diff":"@@ -0,0 +12,1 @@\n+added line"}
				]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/authentication/validate":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"valid":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"issues":[
					{"key":"ISSUE-LOG-1","rule":"go:S100","type":"CODE_SMELL","severity":"MAJOR","message":"inline issue message","component":"project:main.go","line":12}
				],
				"paging":{"pageIndex":1,"pageSize":500,"total":1}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/qualitygates/project_status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"projectStatus":{"status":"OK"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/measures/component":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"component":{"measures":[{"metric":"coverage","value":"80.5"},{"metric":"new_coverage","value":"70.0"}]}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var output bytes.Buffer
	err := runWith(
		[]string{
			"--sonar-url=" + server.URL,
			"--sonar-token=token",
			"--sonar-project-key=project",
			"--gitlab-url=" + server.URL,
			"--gitlab-token=token",
			"--project-id=100",
			"--mr-iid=42",
			"--dry-run",
			"--logs=true",
		},
		func(string) string { return "" },
		&output,
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	logOutput := output.String()
	for _, expected := range []string{
		"Fetched SonarQube issues: 1",
		`Sonar issue #1: key="ISSUE-LOG-1"`,
		`severity="MAJOR"`,
		`type="CODE_SMELL"`,
	} {
		if !strings.Contains(logOutput, expected) {
			t.Fatalf("output %q does not contain %q", logOutput, expected)
		}
	}
}

func TestRunWithInlineInvalidPositionFallsBackToSummary(t *testing.T) {
	t.Parallel()

	summaryNotesCreated := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/100/merge_requests/42":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"iid":42,"diff_refs":{"base_sha":"base","start_sha":"start","head_sha":"head"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/100/merge_requests/42/changes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"changes":[
					{"old_path":"main.go","new_path":"main.go","diff":"@@ -0,0 +12,1 @@\n+added line"}
				]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/authentication/validate":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"valid":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"issues":[
					{"key":"ISSUE-LINE-CODE","rule":"go:S100","type":"CODE_SMELL","severity":"MAJOR","message":"inline issue","component":"project:main.go","line":12}
				],
				"paging":{"pageIndex":1,"pageSize":500,"total":1}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/qualitygates/project_status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"projectStatus":{"status":"OK"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/measures/component":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"component":{"measures":[{"metric":"coverage","value":"80.5"},{"metric":"new_coverage","value":"70.0"}]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/100/merge_requests/42/discussions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects/100/merge_requests/42/discussions":
			http.Error(
				w,
				`{"message":"400 Bad request - Note {:line_code=>[\"can't be blank\", \"must be a valid line code\"]}"}`,
				http.StatusBadRequest,
			)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/100/merge_requests/42/notes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects/100/merge_requests/42/notes":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("failed to parse form: %v", err)
			}
			body := r.PostForm.Get("body")
			if !strings.Contains(body, "without line binding") {
				t.Fatalf("expected summary to contain project-level section, got %q", body)
			}
			if !strings.Contains(body, "inline issue") {
				t.Fatalf("expected summary to include skipped issue message, got %q", body)
			}
			summaryNotesCreated++
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var output bytes.Buffer
	err := runWith(
		[]string{
			"--sonar-url=" + server.URL,
			"--sonar-token=token",
			"--sonar-project-key=project",
			"--gitlab-url=" + server.URL,
			"--gitlab-token=token",
			"--project-id=100",
			"--mr-iid=42",
		},
		func(string) string { return "" },
		&output,
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if summaryNotesCreated != 1 {
		t.Fatalf("expected one summary note create, got %d", summaryNotesCreated)
	}

	logOutput := output.String()
	for _, expected := range []string{
		"Action log: found 1 issues, published 1 comments",
		"Posted 0 inline SonarQube discussions to merge request 42",
	} {
		if !strings.Contains(logOutput, expected) {
			t.Fatalf("output %q does not contain %q", logOutput, expected)
		}
	}
}

func assertCommentContains(t *testing.T, comment, expected string) {
	t.Helper()

	if !strings.Contains(comment, expected) {
		t.Fatalf("comment %q does not contain %q", comment, expected)
	}
}
