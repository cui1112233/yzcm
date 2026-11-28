package batchfactoryv11

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrTextOutputTruncated = errDirectorOutputTruncated
var ErrTextRefused = errors.New("模型拒绝返回正文（refusal），请检查内容后手动重试")
var ErrTextResultUnknown = errors.New("模型结果未知，请核对后手动重试")
var ErrTextEmpty = errors.New("文本模型返回了空正文")

// DecodeTextCompletionResponse preserves completion evidence for scheduling.
func DecodeTextCompletionResponse(raw []byte) (string, error) {
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
			Text         string `json:"text"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.Choices) == 0 {
		return "", fmt.Errorf("%w：响应格式无效", ErrTextResultUnknown)
	}
	choice := decoded.Choices[0]
	switch strings.ToLower(strings.TrimSpace(choice.FinishReason)) {
	case "length", "max_tokens":
		return "", fmt.Errorf("%w (finish_reason=length)", ErrTextOutputTruncated)
	case "content_filter", "refusal":
		return "", ErrTextRefused
	}
	if strings.TrimSpace(choice.Message.Refusal) != "" {
		return "", ErrTextRefused
	}
	content := strings.TrimSpace(choice.Message.Content)
	if content == "" {
		content = strings.TrimSpace(choice.Text)
	}
	if content == "" {
		return "", ErrTextEmpty
	}
	return content, nil
}
