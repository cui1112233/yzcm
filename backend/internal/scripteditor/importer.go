package scripteditor

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const maxImportBytes = 20 << 20

var ErrImportTooLarge = errors.New("导入文件超过20MiB")

type ImportSection struct {
	ID        string `json:"id"`
	Number    int    `json:"number,omitempty"`
	Unit      string `json:"unit,omitempty"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	WordCount int    `json:"wordCount"`
	Preview   string `json:"preview"`
}

type ImportPreview struct {
	Filename     string          `json:"filename"`
	Recognized   bool            `json:"recognized"`
	InferredType WorkType        `json:"inferredType"`
	Sections     []ImportSection `json:"sections"`
}

var (
	chineseHeading = regexp.MustCompile(`^第\s*([零〇○一二两三四五六七八九十百千万两0-9]+)\s*([章回节卷集幕部篇])\s*(.*)$`)
	englishHeading = regexp.MustCompile(`(?i)^(chapter|ch\.?|episode|ep)\s*[\.．　 ]*0*([0-9]+)\s*(.*)$`)
	markdownPrefix = regexp.MustCompile(`^#{1,6}\s*`)
)

func PreviewImport(filename string, content []byte) (ImportPreview, error) {
	if len(content) > maxImportBytes {
		return ImportPreview{}, ErrImportTooLarge
	}
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	var text string
	var err error
	switch ext {
	case ".txt", ".md", ".markdown":
		text, err = decodeImportText(content)
	case ".docx":
		text, err = readDOCX(content)
	case ".doc":
		return ImportPreview{}, fmt.Errorf("%w: 暂不支持旧DOC，请另存为DOCX后导入", ErrValidation)
	default:
		return ImportPreview{}, fmt.Errorf("%w: 不支持的文件格式%s", ErrValidation, ext)
	}
	if err != nil {
		return ImportPreview{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"))
	if text == "" {
		return ImportPreview{}, fmt.Errorf("%w: 文件没有可导入正文", ErrValidation)
	}
	sections, recognized, inferred := splitImportSections(text)
	return ImportPreview{Filename: filepath.Base(filename), Recognized: recognized, InferredType: inferred, Sections: sections}, nil
}

func decodeImportText(content []byte) (string, error) {
	content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	if utf8.Valid(content) {
		return string(content), nil
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(content)
	if err != nil || !utf8.Valid(decoded) {
		return "", errors.New("无法识别文件编码")
	}
	return string(decoded), nil
}

func readDOCX(content []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", errors.New("DOCX文件已损坏")
	}
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			return "", err
		}
		defer stream.Close()
		return docxText(stream)
	}
	return "", errors.New("DOCX中没有正文")
}

func docxText(reader io.Reader) (string, error) {
	decoder := xml.NewDecoder(reader)
	var out strings.Builder
	inText := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", errors.New("DOCX正文XML无法解析")
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "t":
				inText = true
			case "tab":
				out.WriteByte('\t')
			case "br":
				out.WriteByte('\n')
			}
		case xml.EndElement:
			if value.Name.Local == "t" {
				inText = false
			}
			if value.Name.Local == "p" {
				out.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				out.Write([]byte(value))
			}
		}
	}
	return strings.TrimSpace(out.String()), nil
}

type parsedHeading struct {
	number int
	unit   string
	title  string
	script bool
}

func parseHeading(line string) (parsedHeading, bool) {
	line = strings.TrimSpace(markdownPrefix.ReplaceAllString(strings.TrimSpace(line), ""))
	line = strings.Trim(line, "【】[]［］ ")
	if match := chineseHeading.FindStringSubmatch(line); match != nil {
		number, ok := chineseNumber(match[1])
		if !ok {
			return parsedHeading{}, false
		}
		return parsedHeading{number: number, unit: match[2], title: line, script: match[2] == "集"}, true
	}
	if match := englishHeading.FindStringSubmatch(line); match != nil {
		number, _ := strconv.Atoi(match[2])
		prefix := strings.ToLower(match[1])
		return parsedHeading{number: number, unit: match[1], title: line, script: strings.HasPrefix(prefix, "ep")}, true
	}
	return parsedHeading{}, false
}

func splitImportSections(text string) ([]ImportSection, bool, WorkType) {
	lines := strings.Split(text, "\n")
	type start struct {
		index   int
		heading parsedHeading
	}
	starts := []start{}
	for index, line := range lines {
		if heading, ok := parseHeading(line); ok {
			starts = append(starts, start{index: index, heading: heading})
		}
	}
	if len(starts) == 0 {
		return []ImportSection{newImportSection(1, 0, "", "全文", text)}, false, WorkNovel
	}
	sections := []ImportSection{}
	if preface := strings.TrimSpace(strings.Join(lines[:starts[0].index], "\n")); preface != "" {
		sections = append(sections, newImportSection(len(sections)+1, 0, "", "前言", preface))
	}
	inferred := WorkNovel
	for index, current := range starts {
		end := len(lines)
		if index+1 < len(starts) {
			end = starts[index+1].index
		}
		body := strings.TrimSpace(strings.Join(lines[current.index+1:end], "\n"))
		sections = append(sections, newImportSection(len(sections)+1, current.heading.number, current.heading.unit, current.heading.title, body))
		if current.heading.script {
			inferred = WorkScript
		}
	}
	return sections, true, inferred
}

func newImportSection(index, number int, unit, title, content string) ImportSection {
	preview := []rune(strings.Join(strings.Fields(content), " "))
	if len(preview) > 80 {
		preview = append(preview[:80], '…')
	}
	return ImportSection{ID: fmt.Sprintf("section-%d", index), Number: number, Unit: unit, Title: strings.TrimSpace(title), Content: content, WordCount: countWords(content), Preview: string(preview)}
}

func chineseNumber(value string) (int, bool) {
	if number, err := strconv.Atoi(value); err == nil {
		return number, number > 0
	}
	digits := map[rune]int{'零': 0, '〇': 0, '○': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	units := map[rune]int{'十': 10, '百': 100, '千': 1000, '万': 10000}
	total, section, current := 0, 0, 0
	for _, r := range value {
		if digit, ok := digits[r]; ok {
			current = digit
			continue
		}
		unit, ok := units[r]
		if !ok {
			return 0, false
		}
		if unit == 10000 {
			section += current
			if section == 0 {
				section = 1
			}
			total, section, current = total+section*unit, 0, 0
			continue
		}
		if current == 0 {
			current = 1
		}
		section, current = section+current*unit, 0
	}
	result := total + section + current
	return result, result > 0
}
