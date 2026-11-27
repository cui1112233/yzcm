package scripteditor

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ExportInput struct {
	Format   string `json:"format"`
	Episodes []int  `json:"episodes,omitempty"`
}

type ExportResult struct {
	Filename    string
	ContentType string
	Content     []byte
}

type exportPayload struct {
	Title      string   `json:"title"`
	ExportedAt string   `json:"exportedAt"`
	Scripts    []Script `json:"scripts"`
}

func (s *Service) Export(ctx context.Context, owner, workID string, input ExportInput) (ExportResult, error) {
	if s == nil || s.Store == nil {
		return ExportResult{}, ErrValidation
	}
	detail, err := s.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return ExportResult{}, err
	}
	wanted := map[int]bool{}
	for _, episode := range input.Episodes {
		if episode < 1 || wanted[episode] {
			return ExportResult{}, ErrValidation
		}
		wanted[episode] = true
	}
	byEpisode := map[int][]Script{}
	for _, script := range detail.Scripts {
		byEpisode[script.Episode] = append(byEpisode[script.Episode], script)
	}
	selected := make([]Script, 0, len(byEpisode))
	for episode, versions := range byEpisode {
		if len(wanted) > 0 && !wanted[episode] {
			continue
		}
		found := false
		for _, script := range versions {
			if script.Preferred {
				selected = append(selected, script)
				found = true
				break
			}
		}
		if !found {
			return ExportResult{}, fmt.Errorf("%w: 第%d集还没有正式版", ErrValidation, episode)
		}
	}
	if len(selected) == 0 {
		return ExportResult{}, fmt.Errorf("%w: 没有可导出的正式版", ErrValidation)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Episode < selected[j].Episode })
	base := safeExportName(detail.Title)
	payload := exportPayload{Title: detail.Title, ExportedAt: time.Now().UTC().Format(time.RFC3339), Scripts: selected}
	switch strings.ToLower(strings.TrimSpace(input.Format)) {
	case "txt":
		var out strings.Builder
		for index, script := range selected {
			if index > 0 {
				out.WriteString("\n\n")
			}
			fmt.Fprintf(&out, "===== 第%d集 · V%d =====\n%s", script.Episode, script.Version, script.Content)
		}
		return ExportResult{Filename: base + "-正式剧本.txt", ContentType: "text/plain; charset=utf-8", Content: []byte(out.String())}, nil
	case "json":
		content, _ := json.MarshalIndent(payload, "", "  ")
		return ExportResult{Filename: base + "-正式剧本.json", ContentType: "application/json; charset=utf-8", Content: content}, nil
	case "zip":
		var out bytes.Buffer
		archive := zip.NewWriter(&out)
		for _, script := range selected {
			file, _ := archive.Create(fmt.Sprintf("第%03d集-V%d.txt", script.Episode, script.Version))
			_, _ = file.Write([]byte(script.Content))
		}
		metadata, _ := archive.Create("metadata.json")
		raw, _ := json.MarshalIndent(payload, "", "  ")
		_, _ = metadata.Write(raw)
		if err := archive.Close(); err != nil {
			return ExportResult{}, err
		}
		return ExportResult{Filename: base + "-正式剧本.zip", ContentType: "application/zip", Content: out.Bytes()}, nil
	default:
		return ExportResult{}, fmt.Errorf("%w: 导出格式只支持txt、json或zip", ErrValidation)
	}
}

func safeExportName(value string) string {
	value = strings.TrimSpace(value)
	for _, token := range []string{"/", `\`, ":", "*", "?", `"`, "<", ">", "|"} {
		value = strings.ReplaceAll(value, token, "_")
	}
	value = strings.Trim(filepath.Base(value), ". ")
	if value == "" {
		return "剧本"
	}
	return value
}
