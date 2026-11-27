package scripteditor

import (
	"strings"
	"testing"
)

func TestNormalizeTitleTrimsAndRejectsInvalidLength(t *testing.T) {
	got, err := NormalizeTitle("  第一部作品  ")
	if err != nil {
		t.Fatalf("NormalizeTitle returned error: %v", err)
	}
	if got != "第一部作品" {
		t.Fatalf("title = %q, want %q", got, "第一部作品")
	}
	if _, err := NormalizeTitle("   "); err == nil {
		t.Fatal("blank title accepted")
	}
	if _, err := NormalizeTitle(strings.Repeat("剧", 121)); err == nil {
		t.Fatal("121-rune title accepted")
	}
}

func TestWorkAndJobStatusesRejectUnknownValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
	}{
		{name: string(WorkNovel), valid: WorkNovel.Valid()},
		{name: string(WorkScript), valid: WorkScript.Valid()},
		{name: "video", valid: WorkType("video").Valid()},
	} {
		if tc.name == "video" && tc.valid {
			t.Fatal("unknown work type accepted")
		}
		if tc.name != "video" && !tc.valid {
			t.Fatalf("known work type %q rejected", tc.name)
		}
	}
	if !JobPartial.Valid() || JobStatus("waiting").Valid() {
		t.Fatal("job status validation is incorrect")
	}
}
