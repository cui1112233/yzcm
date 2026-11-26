package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ac "qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/novelfetchworkshop"
)

type integrationTestQueue struct {
	*novelfetchworkshop.MemoryQueue
}

// Leave the real in-memory enqueue path intact. Only the test can drain it,
// so route assertions are independent of the background worker.
func (q integrationTestQueue) Claim(ctx context.Context, wait time.Duration) (novelfetchworkshop.ProcessJob, error) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return novelfetchworkshop.ProcessJob{}, ctx.Err()
	case <-timer.C:
		return novelfetchworkshop.ProcessJob{}, novelfetchworkshop.ErrQueueEmpty
	}
}

func TestAccountCenterBusinessExemptionsKeepExecutorLifecycleAndHistoricalReads(t *testing.T) {
	for _, route := range []struct {
		method, path string
		check        bool
	}{
		{"GET", "/api/integrations/changdu/bookid-fetch/status", false},
		{"POST", "/api/integrations/workbench/push", true},
		{"POST", "/api/integrations/changdu/bookid-fetch/run", true},
		{"POST", "/api/local-executor/v1/pair", false},
		{"POST", "/api/local-executor/v1/jobs/job/artifact", false},
		{"POST", "/api/giant-material-executor/v1/jobs/job/result", false},
		{"POST", "/api/local-executor/v1/jobs/job/start", true},
		{"GET", "/api/local-executor/v1/artifacts/artifact", false},
		{"POST", "/api/batch-factory/v12/batches/batch/merge-media/old/token", false},
	} {
		if got := accountCenterBusinessWrite(httptest.NewRequest(route.method, route.path, nil)); got != route.check {
			t.Errorf("%s %s requires check = %t want %t", route.method, route.path, got, route.check)
		}
	}
}

func TestAccountCenterNovelFetchIntegrationChecksFinalAccountBeforeCreatingOrQueuing(t *testing.T) {
	for _, route := range []struct {
		path    string
		payload string
	}{
		{"/api/integrations/workbench/push", `{"books":[{"book_id":"1234567890"}]}`},
		{"/api/integrations/changdu/bookid-fetch/run", `{"input_text":"1234567890"}`},
	} {
		t.Run(route.path, func(t *testing.T) {
			now := time.Now()
			accounts := &governanceAuthStore{accounts: map[string]auth.Account{
				"owner":  {Username: "owner", IsOwner: true, Active: true},
				"reader": {Username: "reader", Active: true},
			}}
			center := &directoryTestStore{directory: ac.AdminDirectory{
				People: []ac.Identity{
					{Username: "owner", Role: ac.RoleFounder, Active: true},
					{Username: "reader", Role: ac.RoleMember, Active: true},
				},
				Teams:       []ac.Team{{ID: "archived", ArchivedAt: &now}, {ID: "active"}},
				Memberships: []ac.Membership{{TeamID: "archived", Username: "reader", EndedAt: &now}},
			}}
			center.resolve = func(_ context.Context, name string) (ac.Identity, error) {
				return (adminSnapshot{AdminDirectory: center.directory}).person(name), nil
			}
			docs := novelfetchworkshop.NewMemoryStore()
			queue := novelfetchworkshop.NewMemoryQueue()
			router := NewRouter(RouterOptions{
				AuthStore: accounts, AccountCenterStore: center, MemberProfileStore: NewMemberProfileStore(t.TempDir()),
				NovelFetchStore: docs, NovelFetchQueue: integrationTestQueue{queue},
				SessionResolver: func(_ context.Context, token string) (BridgeIdentity, error) {
					account, ok := accounts.accounts[token]
					if !ok {
						return BridgeIdentity{}, auth.ErrNotFound
					}
					return BridgeIdentity{Username: account.Username, IsOwner: account.IsOwner}, nil
				},
			})
			request := func(method, path, token, scope, body string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+token)
				if scope != "" {
					req.Header.Set(HeaderNovelFetchAccount, scope)
				}
				resp := httptest.NewRecorder()
				router.ServeHTTP(resp, req)
				return resp
			}
			const statusPath = "/api/integrations/changdu/bookid-fetch/status"
			before := request("GET", statusPath, "reader", "", "")
			if before.Code != 200 {
				t.Fatalf("read status = %d %s", before.Code, before.Body)
			}
			for _, actor := range []string{"reader", "owner"} {
				response := request("POST", route.path, actor, "reader", route.payload)
				if response.Code != http.StatusForbidden {
					t.Errorf("archived write by %s = %d %s", actor, response.Code, response.Body)
				}
			}
			for _, owner := range []string{"reader", "owner"} {
				if _, err := docs.GetDocument(context.Background(), owner, "1234567890"); !errors.Is(err, novelfetchworkshop.ErrNotFound) {
					t.Errorf("rejected integration left a document for %s: %v", owner, err)
				}
				if runs, err := docs.ListRuns(context.Background(), owner, ""); err != nil || len(runs) != 0 {
					t.Errorf("rejected integration left runs for %s: %+v, %v", owner, runs, err)
				}
			}
			if job, err := queue.Claim(context.Background(), time.Millisecond); !errors.Is(err, novelfetchworkshop.ErrQueueEmpty) {
				t.Errorf("rejected integration left a job: %+v, %v", job, err)
			}
			if after := request("GET", statusPath, "reader", "", ""); after.Code != 200 || after.Body.String() != before.Body.String() {
				t.Errorf("rejected integration changed status: %d %s, before %s", after.Code, after.Body, before.Body)
			}
			if t.Failed() {
				return
			}
			center.directory.Memberships = append(center.directory.Memberships, ac.Membership{TeamID: "active", Username: "reader"})
			for _, actor := range []struct{ username, bookID string }{{"reader", "1234567890"}, {"owner", "2234567890"}} {
				response := request("POST", route.path, actor.username, "reader", strings.ReplaceAll(route.payload, "1234567890", actor.bookID))
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"accepted":1`) {
					t.Fatalf("new active team integration by %s = %d %s", actor.username, response.Code, response.Body)
				}
				doc, err := docs.GetDocument(context.Background(), "reader", actor.bookID)
				if err != nil || novelfetchworkshop.DeriveTaskStatus(doc) != novelfetchworkshop.TaskStatusQueued {
					t.Fatalf("integration document under final account = %+v, %v", doc, err)
				}
				job, err := queue.Claim(context.Background(), time.Millisecond)
				if err != nil || job.Owner != "reader" || len(job.BookIDs) != 1 || job.BookIDs[0] != actor.bookID {
					t.Fatalf("queued final account = %+v, %v", job, err)
				}
			}
		})
	}
}
