package sonar

import "testing"

func TestFilterIssuesBySeverity(t *testing.T) {
	t.Parallel()

	issues := []Issue{
		{Key: "I", Severity: "INFO"},
		{Key: "M", Severity: "MINOR"},
		{Key: "J", Severity: "MAJOR"},
		{Key: "C", Severity: "CRITICAL"},
		{Key: "B", Severity: "BLOCKER"},
		{Key: "U", Severity: "UNKNOWN"},
	}

	filtered := FilterIssuesBySeverity(issues, "MAJOR")
	if len(filtered) != 3 {
		t.Fatalf("expected 3 issues at MAJOR+, got %d", len(filtered))
	}

	got := []string{filtered[0].Key, filtered[1].Key, filtered[2].Key}
	want := []string{"J", "C", "B"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected filtered order/content: got %v want %v", got, want)
		}
	}
}

func TestFilterIssuesBySeverityReturnsAllWithoutThreshold(t *testing.T) {
	t.Parallel()

	issues := []Issue{
		{Key: "A", Severity: "INFO"},
		{Key: "B", Severity: "BLOCKER"},
	}

	filtered := FilterIssuesBySeverity(issues, "")
	if len(filtered) != len(issues) {
		t.Fatalf("expected %d issues, got %d", len(issues), len(filtered))
	}

	for i := range issues {
		if filtered[i] != issues[i] {
			t.Fatalf("unexpected issue at index %d: got %+v want %+v", i, filtered[i], issues[i])
		}
	}
}

func TestIsValidSeverity(t *testing.T) {
	t.Parallel()

	if !IsValidSeverity("critical") {
		t.Fatal("expected critical to be a valid severity")
	}
	if IsValidSeverity("SEVERE") {
		t.Fatal("expected SEVERE to be invalid")
	}
}
