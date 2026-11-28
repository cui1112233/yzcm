package screenplay

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func textScope(t *testing.T, actual string) AssistantReferenceScope {
	t.Helper()
	s, e := NewAssistantReferenceScope("owner", "project", "default", "member", actual)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestAssistantTextAttachmentScope(t *testing.T) {
	s := textScope(t, "")
	if s.ValidateRead() != nil || !errors.Is(s.ValidatePrepare(), ErrAccessDenied) {
		t.Fatal("read/prepare separation")
	}
	raw, _ := json.Marshal(s)
	if string(raw) != "{}" {
		t.Fatal("scope serializable")
	}
	var forged AssistantReferenceScope
	_ = json.Unmarshal([]byte(`{"owner":"owner","project":"project","session":"default","effective":"member","actual":"member"}`), &forged)
	if forged.ValidateRead() == nil {
		t.Fatal("JSON scope")
	}
	for _, v := range [][5]string{{"", "p", "default", "u", "a"}, {"o", "", "default", "u", "a"}, {"o", "p", "", "u", "a"}, {"o", "p", "default", "", "a"}} {
		if _, e := NewAssistantReferenceScope(v[0], v[1], v[2], v[3], v[4]); e == nil {
			t.Fatal("missing scope")
		}
	}
}
func TestAssistantTextAttachmentBytes(t *testing.T) {
	original := "\ufeff\r\n  # 标题\r\n\r\n    ```md\r\n<a>&\\\"\r\n    ```\r\n\r\n"
	a, e := PrepareAssistantTextAttachment(textScope(t, "actual"), "id", "n.md", strings.Repeat("a", 64), []byte(original))
	if e != nil {
		t.Fatal(e)
	}
	if a.Text != strings.TrimPrefix(original, "\ufeff") || a.RawSHA256 == a.TextSHA256 || a.RawBytes != len(original) {
		t.Fatal("changed bytes")
	}
	snap, e := a.Snapshot()
	if e != nil || snap.Attachment == nil || snap.Bytes != len(a.Text) {
		t.Fatal(snap, e)
	}
	if _, e = (AssistantReferenceView{}).Describe(snap.Selector()); !errors.Is(e, ErrInvalidArgument) {
		t.Fatal("unscoped", e)
	}
	for _, v := range []struct {
		name string
		data []byte
	}{{"x.txt", nil}, {"x.md", []byte(" \r\n")}, {"x.txt", []byte{255}}, {"x.txt", []byte{'a', 0}}, {"../x.txt", []byte("x")}, {"x.pdf", []byte("x")}, {"x.txt", bytes.Repeat([]byte("x"), AssistantTextAttachmentRawBytes+1)}} {
		if _, e := PrepareAssistantTextAttachment(textScope(t, "actual"), "id", v.name, strings.Repeat("a", 64), v.data); e == nil {
			t.Fatal("invalid accepted", v.name, len(v.data))
		}
	}
	if _, e := PrepareAssistantTextAttachment(textScope(t, "actual"), "id", "x.txt", strings.Repeat("a", 64), bytes.Repeat([]byte("x"), AssistantTextAttachmentRawBytes)); e == nil {
		t.Fatal("raw max bypassed item budget")
	}
	last := 0
	for n := 1; n < 5000; n++ {
		a, e := PrepareAssistantTextAttachment(textScope(t, "actual"), "id", "x.txt", strings.Repeat("a", 64), bytes.Repeat([]byte("<"), n))
		if e != nil {
			if last == 0 {
				t.Fatal(e)
			}
			break
		}
		snap, _ := a.Snapshot()
		b, _ := json.Marshal(snap)
		if len(b) > AssistantReferenceItemBytes {
			t.Fatal("JSON over budget")
		}
		last = n
	}
	if last == 0 || last >= 4000 {
		t.Fatal("escaping ignored", last)
	}
}
func TestAssistantTextAttachmentVisibility(t *testing.T) {
	s := textScope(t, "actual")
	a, e := PrepareAssistantTextAttachment(s, "id", "x.txt", strings.Repeat("a", 64), []byte("hello"))
	if e != nil {
		t.Fatal(e)
	}
	if a.Visible(s) {
		t.Fatal("unready")
	}
	a.Status = "ready"
	a.Verified = true
	if !a.Visible(s) {
		t.Fatal("own draft")
	}
	other, _ := NewAssistantReferenceScope("owner", "project", "default", "other", "real-other")
	if a.Visible(other) {
		t.Fatal("draft shared")
	}
	before, _ := a.Snapshot()
	a.Published = true
	a.StateRevision++
	after, _ := a.Snapshot()
	if before.Digest != after.Digest || !a.Visible(other) {
		t.Fatal("publication changed content")
	}
	cross, _ := NewAssistantReferenceScope("owner", "project", "private", "other", "real-other")
	if a.Visible(cross) {
		t.Fatal("cross session")
	}
	a.Status = "archived"
	if a.Visible(s) {
		t.Fatal("archived")
	}
}

func TestAssistantTextAttachmentExactJSONBoundary(t *testing.T) {
	s := textScope(t, "actual")
	ns := strings.Repeat("a", 64)
	a, e := PrepareAssistantTextAttachment(s, "id", "x.txt", ns, []byte("x"))
	if e != nil {
		t.Fatal(e)
	}
	snapshot, _ := a.Snapshot()
	raw, _ := json.Marshal(snapshot)
	n := AssistantReferenceItemBytes - len(raw) + 1
	for i := 0; i < 4; i++ {
		probe := snapshot
		probe.Text = strings.Repeat("x", n)
		probe.Bytes = n
		b, _ := json.Marshal(probe)
		n += AssistantReferenceItemBytes - len(b)
	}
	a, e = PrepareAssistantTextAttachment(s, "id", "x.txt", ns, bytes.Repeat([]byte("x"), n))
	if e != nil {
		t.Fatal("exact boundary rejected", e)
	}
	snapshot, _ = a.Snapshot()
	raw, _ = json.Marshal(snapshot)
	if len(raw) != AssistantReferenceItemBytes {
		t.Fatal("not exact", len(raw))
	}
	if _, e = PrepareAssistantTextAttachment(s, "id", "x.txt", ns, bytes.Repeat([]byte("x"), n+1)); e == nil {
		t.Fatal("one byte excess accepted")
	}
	for _, raw := range [][]byte{[]byte("text\x01binary"), []byte("\x7fdata")} {
		if _, e = PrepareAssistantTextAttachment(s, "id", "x.txt", ns, raw); e == nil {
			t.Fatal("binary control accepted")
		}
	}
}
