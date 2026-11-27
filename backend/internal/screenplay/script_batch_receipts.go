package screenplay

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// Keys come exclusively from the authenticated owner and persisted server run.
// No endpoint accepts a response body or a receipt path from the browser.
type ScriptBatchReceiptKey struct {
	Owner       string `json:"owner"`
	Project     string `json:"project"`
	Chapter     string `json:"chapter"`
	Generation  string `json:"generation"`
	InputDigest string `json:"input_digest"`
	Batch       int    `json:"batch"`
}
type ScriptBatchReceipt struct {
	Key       ScriptBatchReceiptKey `json:"key"`
	Ready     bool                  `json:"ready"`
	RawResult string                `json:"raw_result"`
	RawDigest string                `json:"raw_digest"`
	RawBytes  int                   `json:"raw_bytes"`
	Error     string                `json:"error,omitempty"`
	Oversized bool                  `json:"oversized,omitempty"`
	Preview   string                `json:"preview,omitempty"`
}
type ScriptBatchReceipts interface {
	Save(context.Context, ScriptBatchReceipt) error
	Load(context.Context, ScriptBatchReceiptKey) (ScriptBatchReceipt, error)
}

func receiptKey(o, p, c string, g *ScriptGeneration) ScriptBatchReceiptKey {
	n := g.RequestedBatch
	if n == 0 {
		n = len(g.Batches) + 1
	}
	return ScriptBatchReceiptKey{Owner: o, Project: p, Chapter: c, Generation: g.ID, InputDigest: g.InputDigest, Batch: n}
}
func boundedScriptDiagnostic(value string, n int) string {
	if len(value) <= n {
		return value
	}
	value = value[:n]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
func returnedScriptReceipt(key ScriptBatchReceiptKey, raw string, err error) ScriptBatchReceipt {
	r := ScriptBatchReceipt{Key: key, Ready: true, RawResult: raw, RawDigest: scriptDigest(raw), RawBytes: len(raw)}
	if err != nil {
		r.Error = boundedScriptDiagnostic(err.Error(), 4096)
	}
	if len(raw) > maxScriptBatchBytes {
		r.RawResult = ""
		r.Preview = boundedScriptDiagnostic(raw, 2048)
		r.Oversized = true
	}
	return r
}
func validateReceipt(r ScriptBatchReceipt) error {
	k := r.Key
	if k.Owner == "" || k.Project == "" || k.Chapter == "" || k.Generation == "" || len(k.InputDigest) != 64 || k.Batch < 1 || k.Batch > maxScriptBatches {
		return ErrInvalidArgument
	}
	if len(k.Owner) > 1024 || len(k.Project) > 1024 || len(k.Chapter) > 1024 || len(k.Generation) > 1024 {
		return ErrInvalidArgument
	}
	if decoded, e := hex.DecodeString(k.InputDigest); e != nil || len(decoded) != 32 {
		return ErrInvalidArgument
	}
	if len(r.RawResult) > maxScriptBatchBytes || len(r.Preview) > 2048 || len(r.Error) > 4096 {
		return fmt.Errorf("%w: 回包收据超过上限", ErrInvalidArgument)
	}
	if r.Ready && !r.Oversized && (r.RawBytes != len(r.RawResult) || r.RawDigest != scriptDigest(r.RawResult)) {
		return fmt.Errorf("%w: 回包收据摘要不匹配", ErrInvalidState)
	}
	if r.Oversized && (r.RawResult != "" || r.RawBytes <= maxScriptBatchBytes || len(r.RawDigest) != 64) {
		return ErrInvalidArgument
	}
	return nil
}

// File receipts are independent of MySQL availability and survive process
// replacement. Production wires Root from its configured UserDataDir, inside
// the screenplay module. Pending receipts also verify disk access before billing.
type FileScriptBatchReceipts struct {
	Root string
	mu   sync.Mutex
}

func (r *FileScriptBatchReceipts) receiptPath(k ScriptBatchReceiptKey) (string, error) {
	if strings.TrimSpace(r.Root) == "" {
		return "", fmt.Errorf("%w: 未配置剧本回包收据目录", ErrInvalidState)
	}
	if e := validateReceipt(ScriptBatchReceipt{Key: k}); e != nil {
		return "", e
	}
	return filepath.Join(r.Root, scriptDigest(k.Owner), scriptDigest(k.Project), scriptDigest(k.Chapter), fmt.Sprintf("%s-%s-%d.json", scriptDigest(k.Generation), k.InputDigest, k.Batch)), nil
}
func (r *FileScriptBatchReceipts) Save(ctx context.Context, value ScriptBatchReceipt) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := validateReceipt(value); e != nil {
		return e
	}
	target, e := r.receiptPath(value.Key)
	if e != nil {
		return e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	readyTarget := target
	if !value.Ready {
		if _, err := os.Stat(readyTarget); err == nil {
			return fmt.Errorf("%w: 回包已返回，请恢复已保存的收据", ErrInvalidState)
		} else if !os.IsNotExist(err) {
			return err
		}
		target += ".pending"
	}
	if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
		return e
	}
	if _, e = os.Stat(target); os.IsNotExist(e) {
		entries, err := os.ReadDir(filepath.Dir(target))
		if err != nil {
			return err
		}
		_, pendingErr := os.Stat(readyTarget + ".pending")
		preparedUpgrade := value.Ready && pendingErr == nil
		if len(entries) >= 1024 && !preparedUpgrade {
			return fmt.Errorf("%w: 章节回包收据数量超过上限", ErrInvalidState)
		}
	}
	// A ready receipt is immutable. An acknowledged response can never be
	// replaced by a newly submitted body, even from another server caller.
	if existing, e := r.load(target, value.Key); e == nil {
		left, _ := json.Marshal(existing)
		right, _ := json.Marshal(value)
		if string(left) != string(right) {
			return fmt.Errorf("%w: 已保存的回包收据不可改写", ErrInvalidState)
		}
		return nil
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(target), "receipt-*.tmp")
	if e != nil {
		return e
	}
	temp := f.Name()
	defer os.Remove(temp)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	// Link installs an already flushed file without ever replacing an existing
	// receipt, including when another process owns a different Go mutex. Pending
	// and ready have distinct paths; a ready receipt is write-once.
	if e = os.Link(temp, target); e != nil {
		if existing, readErr := r.load(target, value.Key); readErr == nil {
			left, _ := json.Marshal(existing)
			if string(left) == string(raw) {
				return nil
			}
		}
		return e
	}
	if value.Ready {
		_ = os.Remove(readyTarget + ".pending")
	}
	// fsync the directory on platforms that support it. Windows rejects directory
	// Sync, while the file itself has already been flushed before atomic install.
	if dir, err := os.Open(filepath.Dir(target)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
func (r *FileScriptBatchReceipts) load(target string, key ScriptBatchReceiptKey) (ScriptBatchReceipt, error) {
	var value ScriptBatchReceipt
	f, e := os.Open(target)
	if e != nil {
		return value, e
	}
	defer f.Close()
	const maxReceiptBytes = maxScriptBatchBytes*6 + 65536
	raw, e := io.ReadAll(io.LimitReader(f, maxReceiptBytes+1))
	if e != nil {
		return value, e
	}
	if len(raw) > maxReceiptBytes {
		return value, ErrInvalidState
	}
	if e = json.Unmarshal(raw, &value); e != nil {
		return value, e
	}
	if value.Key != key {
		return value, fmt.Errorf("%w: 回包收据绑定不匹配", ErrInvalidState)
	}
	return value, validateReceipt(value)
}
func (r *FileScriptBatchReceipts) Load(ctx context.Context, key ScriptBatchReceiptKey) (ScriptBatchReceipt, error) {
	if e := ctx.Err(); e != nil {
		return ScriptBatchReceipt{}, e
	}
	target, e := r.receiptPath(key)
	if e != nil {
		return ScriptBatchReceipt{}, e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	value, e := r.load(target, key)
	if os.IsNotExist(e) {
		return r.load(target+".pending", key)
	}
	return value, e
}

// Used only by the direct route test/embedding entry point. The production
// router always supplies FileScriptBatchReceipts and fails closed if unconfigured.
type MemoryScriptBatchReceipts struct {
	mu   sync.Mutex
	rows map[ScriptBatchReceiptKey]ScriptBatchReceipt
}

func NewMemoryScriptBatchReceipts() *MemoryScriptBatchReceipts {
	return &MemoryScriptBatchReceipts{rows: map[ScriptBatchReceiptKey]ScriptBatchReceipt{}}
}
func (r *MemoryScriptBatchReceipts) Save(_ context.Context, value ScriptBatchReceipt) error {
	if e := validateReceipt(value); e != nil {
		return e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.rows[value.Key]; ok && old.Ready {
		left, _ := json.Marshal(old)
		right, _ := json.Marshal(value)
		if string(left) != string(right) {
			return fmt.Errorf("%w: 已保存的回包收据不可改写", ErrInvalidState)
		}
		return nil
	}
	r.rows[value.Key] = value
	return nil
}
func (r *MemoryScriptBatchReceipts) Load(_ context.Context, key ScriptBatchReceiptKey) (ScriptBatchReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.rows[key]
	if !ok {
		return value, ErrNotFound
	}
	return value, nil
}
