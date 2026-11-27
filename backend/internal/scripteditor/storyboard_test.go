package scripteditor

import "testing"

func TestStoryboardCoverageReportsMissingAndDuplicateSourceRanges(t *testing.T) {
	report := ComputeCoverage(100, []SourceRange{{Start: 0, End: 30}, {Start: 20, End: 50}})
	if len(report.Missing) != 1 || report.Missing[0] != (SourceRange{Start: 50, End: 100}) {
		t.Fatalf("missing = %#v", report.Missing)
	}
	if len(report.Duplicate) != 1 || report.Duplicate[0] != (SourceRange{Start: 20, End: 30}) {
		t.Fatalf("duplicate = %#v", report.Duplicate)
	}
}
