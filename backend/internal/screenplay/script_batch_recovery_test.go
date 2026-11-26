package screenplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"qiantie/backend/internal/prompt"
	"strings"
	"sync"
	"testing"
)

type receiptFaultRepository struct {
	AuthoringRepository
	failLoad, failSave bool
}

func (r *receiptFaultRepository) Load(ctx context.Context, o, p string) (AuthoringState, error) {
	if r.failLoad {
		return AuthoringState{}, errors.New("injected database Load unavailable")
	}
	return r.AuthoringRepository.Load(ctx, o, p)
}
func (r *receiptFaultRepository) Save(ctx context.Context, s AuthoringState, v int64) (AuthoringState, error) {
	if r.failSave {
		return AuthoringState{}, errors.New("injected database Save unavailable")
	}
	return r.AuthoringRepository.Save(ctx, s, v)
}

func TestScriptBatchRecoverAfterDatabaseLostReturnedBody(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	repo := &receiptFaultRepository{AuthoringRepository: svc.Repository}
	svc.Repository = repo
	calls := 0
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
		calls++
		repo.failLoad = true
		return TextResponse{Content: batchFixture(1, false, "剩余。")}, nil
	})
	if _, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false); e == nil {
		t.Fatal("expected injected database failure")
	}
	repo.failLoad = false
	live, e := svc.Load(ctx, "a", "p")
	if e != nil {
		t.Fatal(e)
	}
	recovered, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "recover", "", live.Revision, false)
	if e != nil {
		t.Fatal("returned body cannot recover without paying model again", e)
	}
	if calls != 1 || recovered.Chapters[0].ScriptGeneration.Status != "awaiting_continue" {
		t.Fatal("recovery called model or lost batch")
	}
}

func TestScriptBatchTemplateSwitchRequiresAndAllowsExplicitRestart(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	calls := 0
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
		calls++
		if calls == 1 {
			return TextResponse{Content: batchFixture(1, false, "剩余。")}, nil
		}
		return TextResponse{Content: batchFixture(1, true, "")}, nil
	})
	out, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	p, e := svc.Store.GetProject(ctx, "a", "p")
	if e != nil {
		t.Fatal(e)
	}
	p.PromptID = prompt.CreativeScript144
	if _, e = svc.Store.UpdateProject(ctx, "a", "p", p, p.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", out.Revision, false); e == nil || calls != 1 {
		t.Fatal("template switch bypassed explicit restart")
	}
	if _, e = svc.GenerateScriptBatch(ctx, "a", "p", "c", "continue", "", out.Revision, false); e == nil || calls != 1 {
		t.Fatal("continued with changed template")
	}
	done, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "restart", "", out.Revision, true)
	if e != nil {
		t.Fatal("explicit restart cannot use new template", e)
	}
	if calls != 2 || done.Chapters[0].ScriptGeneration.Status != "completed" || len(done.Chapters[0].ScriptGeneration.History) != 1 || done.Chapters[0].ScriptGeneration.PromptID != prompt.CreativeScript144 {
		t.Fatal("restart lost previous run or selected template")
	}
}

func TestScriptBatchRolloverRejectsDecimalBoundary(t *testing.T) {
	if sentenceRollover("他拿出3.14元。余句。", "14元。余句。") {
		t.Fatal("decimal point accepted as full sentence boundary")
	}
}

type receiptFaultStore struct {
	Store
	failProject, failAssets bool
}

func (s *receiptFaultStore) GetProject(ctx context.Context, o, p string) (Project, error) {
	if s.failProject {
		return Project{}, errors.New("injected project storage unavailable")
	}
	return s.Store.GetProject(ctx, o, p)
}
func (s *receiptFaultStore) ListAssets(ctx context.Context, o, p string, a AssetType) ([]Asset, error) {
	if s.failAssets {
		return nil, errors.New("injected asset storage unavailable")
	}
	return s.Store.ListAssets(ctx, o, p, a)
}

func TestScriptBatchFileReceiptRecoversInFreshServiceAfterEachDatabaseFailure(t *testing.T) {
	for _, failure := range []string{"Load", "GetProject", "ListAssets", "Save"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			svc, st := batchService(t)
			root := t.TempDir()
			svc.Receipts = &FileScriptBatchReceipts{Root: root}
			repo := &receiptFaultRepository{AuthoringRepository: svc.Repository}
			store := &receiptFaultStore{Store: svc.Store}
			svc.Store = store
			svc.Repository = repo
			calls := 0
			svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
				calls++
				switch failure {
				case "Load":
					repo.failLoad = true
				case "Save":
					repo.failSave = true
				case "GetProject":
					store.failProject = true
				case "ListAssets":
					store.failAssets = true
				}
				return TextResponse{Content: batchFixture(1, false, "剩余。")}, nil
			})
			if _, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false); e == nil {
				t.Fatal("fault was not exercised")
			}
			repo.failLoad = false
			repo.failSave = false
			store.failProject = false
			store.failAssets = false
			// No generator and a new file-store object: only on-disk state can recover.
			restarted := AuthoringService{Store: store, Repository: repo, Receipts: &FileScriptBatchReceipts{Root: root}}
			live, e := restarted.Load(ctx, "a", "p")
			if e != nil {
				t.Fatal(e)
			}
			recovered, e := restarted.GenerateScriptBatch(ctx, "a", "p", "c", "recover", "", live.Revision, false)
			if e != nil {
				t.Fatal(e)
			}
			g := recovered.Chapters[0].ScriptGeneration
			if calls != 1 || g.Status != "awaiting_continue" || len(g.Batches) != 1 || g.Batches[0].RawResult != batchFixture(1, false, "剩余。") || recovered.Chapters[0].RawResult != "old" {
				t.Fatal("fresh-service recovery lost raw or called model")
			}
			again, e := restarted.GenerateScriptBatch(ctx, "a", "p", "c", "recover", "", recovered.Revision, false)
			if e != nil || len(again.Chapters[0].ScriptGeneration.Batches) != 1 {
				t.Fatal("recovery duplicated batch", e)
			}
		})
	}
}

func TestScriptBatchReceiptCannotWriteBeforePaidCall(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	blocked := filepath.Join(t.TempDir(), "file")
	if e := os.WriteFile(blocked, []byte("not a directory"), 0600); e != nil {
		t.Fatal(e)
	}
	svc.Receipts = &FileScriptBatchReceipts{Root: blocked}
	calls := 0
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) { calls++; return TextResponse{}, nil })
	if _, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false); e == nil || calls != 0 || strings.Contains(e.Error(), blocked) {
		t.Fatal("unwritable journal billed model or leaked server path", e)
	}
	svc.Receipts = nil
	if _, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false); e == nil || calls != 0 {
		t.Fatal("missing journal billed model")
	}
}

func TestScriptBatchOversizedResponseStoresBoundedReceiptAndDiagnostic(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	root := t.TempDir()
	journal := &FileScriptBatchReceipts{Root: root}
	svc.Receipts = journal
	raw := strings.Repeat("字", maxScriptBatchBytes)
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) { return TextResponse{Content: raw}, nil })
	out, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	g := out.Chapters[0].ScriptGeneration
	batch := g.Batches[0]
	if g.Status != "invalid" || batch.RawResult != "" || len(batch.Preview) > 2048 || batch.RawBytes != len(raw) || batch.RawDigest != scriptDigest(raw) || out.Chapters[0].RawResult != "old" {
		t.Fatal("unbounded raw persisted or success replaced")
	}
	record, e := journal.Load(ctx, receiptKey("a", "p", "c", g))
	if e != nil {
		t.Fatal(e)
	}
	if !record.Oversized || record.RawResult != "" {
		t.Fatal("oversized journal body not bounded")
	}
	target, _ := journal.receiptPath(record.Key)
	info, e := os.Stat(target)
	if e != nil || info.Size() > 16384 {
		t.Fatal("oversized raw enlarged receipt", e)
	}
}

func TestScriptBatchReceiptScopeBindingAndWriteOnceAcrossInstances(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	journal := &FileScriptBatchReceipts{Root: root}
	key := ScriptBatchReceiptKey{Owner: "a", Project: "p", Chapter: "c", Generation: "run", InputDigest: scriptDigest("source"), Batch: 1}
	record := returnedScriptReceipt(key, "raw", nil)
	if e := journal.Save(ctx, record); e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"owner", "project", "chapter", "generation", "input"} {
		other := key
		switch field {
		case "owner":
			other.Owner = "b"
		case "project":
			other.Project = "other"
		case "chapter":
			other.Chapter = "other"
		case "generation":
			other.Generation = "other"
		case "input":
			other.InputDigest = scriptDigest("other")
		}
		if _, e := (&FileScriptBatchReceipts{Root: root}).Load(ctx, other); e == nil {
			t.Fatal("scope read succeeded", field)
		}
	}
	// Even a file placed at the correct hashed location must carry the same
	// complete owner/project/chapter/run/input binding internally.
	target, _ := journal.receiptPath(key)
	tampered := record
	tampered.Key.Owner = "b"
	wire, _ := json.Marshal(tampered)
	if e := os.WriteFile(target, wire, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := journal.Load(ctx, key); e == nil {
		t.Fatal("tampered binding trusted")
	}
	raceKey := key
	raceKey.Generation = "race"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, body := range []string{"first", "second"} {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			results <- (&FileScriptBatchReceipts{Root: root}).Save(ctx, returnedScriptReceipt(raceKey, body, nil))
		}(body)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("independent writers overwrote ready receipt", success)
	}
	saved, e := (&FileScriptBatchReceipts{Root: root}).Load(ctx, raceKey)
	if e != nil || saved.RawResult != "first" && saved.RawResult != "second" {
		t.Fatal("race corrupted receipt", e)
	}
	invalid := key
	invalid.InputDigest = strings.Repeat("../", 21) + "/"
	if _, e := journal.receiptPath(invalid); e == nil {
		t.Fatal("nonhex input digest allowed a path")
	}
}

func TestScriptBatchReceiptPreparedSlotCanAlwaysBecomeReadyAtCountLimit(t *testing.T) {
	ctx := context.Background()
	journal := &FileScriptBatchReceipts{Root: t.TempDir()}
	key := ScriptBatchReceiptKey{Owner: "a", Project: "p", Chapter: "c", Generation: "run", InputDigest: scriptDigest("source"), Batch: 1}
	target, _ := journal.receiptPath(key)
	dir := filepath.Dir(target)
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 1023; i++ {
		if e := os.WriteFile(filepath.Join(dir, fmt.Sprintf("existing-%d.json", i)), nil, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := journal.Save(ctx, ScriptBatchReceipt{Key: key}); e != nil {
		t.Fatal(e)
	}
	if e := journal.Save(ctx, returnedScriptReceipt(key, "returned", nil)); e != nil {
		t.Fatal("prepared slot cannot persist paid response at quota boundary", e)
	}
}

type failedPublicationMarkerRepository struct {
	AuthoringRepository
	fail bool
}

func (r *failedPublicationMarkerRepository) Save(ctx context.Context, st AuthoringState, v int64) (AuthoringState, error) {
	if r.fail {
		for _, c := range st.Chapters {
			if c.ScriptGeneration != nil && c.ScriptGeneration.Status == "completed" && c.ScriptGeneration.PublicationError != "" {
				return AuthoringState{}, errors.New("injected publication error marker Save failure")
			}
		}
	}
	return r.AuthoringRepository.Save(ctx, st, v)
}

func TestScriptBatchCompletedWithoutPublicationMarkersCanRecoverFree(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	root := t.TempDir()
	svc.Receipts = &FileScriptBatchReceipts{Root: root}
	repo := &failedPublicationMarkerRepository{AuthoringRepository: svc.Repository, fail: true}
	store := &failingBatchPublishStore{Store: svc.Store, fail: true}
	svc.Repository = repo
	svc.Store = store
	calls := 0
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
		calls++
		return TextResponse{Content: batchFixture(1, true, "")}, nil
	})
	if _, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false); e == nil {
		t.Fatal("publication marker failure was not exercised")
	}
	saved, e := svc.Load(ctx, "a", "p")
	if e != nil {
		t.Fatal(e)
	}
	g := saved.Chapters[0].ScriptGeneration
	if g.Status != "completed" || g.PublicationError != "" || g.PublishedDocumentID != "" || len(g.Batches) != 1 {
		t.Fatal("fixture did not leave completed result with missing publication markers")
	}
	repo.fail = false
	store.fail = false
	fresh := AuthoringService{Store: store, Repository: repo, Receipts: &FileScriptBatchReceipts{Root: root}}
	recovered, e := fresh.GenerateScriptBatch(ctx, "a", "p", "c", "recover", "", saved.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	if calls != 1 || recovered.Chapters[0].ScriptGeneration.PublishedDocumentID == "" {
		t.Fatal("publication recovery called model or failed to publish")
	}
	if _, e = fresh.GenerateScriptBatch(ctx, "a", "p", "c", "publish", "", recovered.Revision, false); e != nil || calls != 1 {
		t.Fatal("free publication action called model or failed", e)
	}
	docs, e := store.ListDocuments(ctx, "a", "p")
	if e != nil || len(docs) != 1 {
		t.Fatal("publication duplicated versions", e, len(docs))
	}
}
