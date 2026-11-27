package scripteditor

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestPreviewImportRecognizesChineseAndEnglishHeadings(t *testing.T) {
	preview, err := PreviewImport("book.txt", []byte("前言\n说明\n第一章 开始\n正文\nChapter 2 Return\nNext"))
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Recognized || len(preview.Sections) != 3 {
		t.Fatalf("preview = %#v", preview)
	}
	if preview.Sections[0].Title != "前言" || preview.Sections[1].Number != 1 || preview.Sections[2].Number != 2 {
		t.Fatalf("sections = %#v", preview.Sections)
	}
	if preview.Sections[2].Title != "Chapter 2 Return" || preview.Sections[2].Content != "Next" {
		t.Fatalf("english section = %#v", preview.Sections[2])
	}
}

func TestPreviewImportFallsBackToWholeTextAndDecodesGB18030(t *testing.T) {
	raw, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("没有章节标题的完整正文"))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewImport("whole.md", raw)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Recognized || len(preview.Sections) != 1 || preview.Sections[0].Title != "全文" || preview.Sections[0].Content != "没有章节标题的完整正文" {
		t.Fatalf("preview = %#v", preview)
	}
}

func TestPreviewImportReadsDOCXParagraphsAndTables(t *testing.T) {
	preview, err := PreviewImport("book.docx", testDOCX(t, `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>第一集 开场</w:t></w:r></w:p>
<w:p><w:r><w:t>人物：甲</w:t></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>场景</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>夜晚</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
</w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Sections) != 1 || preview.Sections[0].Number != 1 || !strings.Contains(preview.Sections[0].Content, "人物：甲") || !strings.Contains(preview.Sections[0].Content, "夜晚") {
		t.Fatalf("preview = %#v", preview)
	}
	if preview.InferredType != WorkScript {
		t.Fatalf("type = %q, want script", preview.InferredType)
	}
}

func TestPreviewImportRejectsOldDOCEmptyAndOversize(t *testing.T) {
	if _, err := PreviewImport("old.doc", []byte("x")); !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "DOCX") {
		t.Fatalf("old doc error = %v", err)
	}
	if _, err := PreviewImport("empty.txt", []byte(" \n\t")); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty error = %v", err)
	}
	if _, err := PreviewImport("large.txt", make([]byte, maxImportBytes+1)); !errors.Is(err, ErrImportTooLarge) {
		t.Fatalf("oversize error = %v", err)
	}
}

func TestCommitImportCreatesOnlySelectedSections(t *testing.T) {
	store := newDeterministicMemoryStore()
	service := NewService(store)
	preview, err := PreviewImport("book.txt", []byte("第一章 甲\n正文甲\n第二章 乙\n正文乙"))
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CommitImport(context.Background(), "alice", ImportCommitInput{
		Title: "测试作品", Type: WorkNovel, Filename: preview.Filename,
		Sections: preview.Sections, SelectedSectionIDs: []string{preview.Sections[1].ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Title != "测试作品" || len(detail.Chapters) != 1 || detail.Chapters[0].Content != "正文乙" {
		t.Fatalf("detail = %#v", detail)
	}
}

func testDOCX(t *testing.T, documentXML string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(documentXML)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
