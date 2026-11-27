package httpapi

import (
	"context"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"io"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/yzcmusage"
	"strings"
	"testing"
)

func TestNovelResolverChecksActiveBindingAndCurrentCatalog(t *testing.T) {
	ctx := context.Background()
	center := newModelOwnerTestCenter()
	catalog := novelfetchworkshop.NewMemoryStore()
	model := map[string]any{"id": "chosen", "modelId": "actual", "kind": "text", "enabled": true, "credential": "secret", "baseUrl": "https://example.invalid/v1"}
	save := func() {
		t.Helper()
		if err := catalog.PutConfig(ctx, "manager", map[string]any{"modelCatalog": []any{model}}); err != nil {
			t.Fatal(err)
		}
	}
	save()
	resolver := NovelCreationTextResolver(center, func(c context.Context, owner, id string) (batchfactoryv11.DirectorProvider, error) {
		return resolveAgentTextModel(c, catalog, nil, owner, id)
	})
	p, err := resolver(ctx, "member", "chosen")
	if err != nil {
		t.Fatal(err)
	}
	if p.(batchfactoryv11.TextModelMetadataProvider).TextModelMetadata().CatalogOwner != "manager" {
		t.Fatal("actual catalog owner was not recorded")
	}
	// All following checks only resolve providers; no external HTTP can be sent.
	member := center.identities["member"]
	member.Active = false
	center.identities["member"] = member
	if _, err = resolver(ctx, "member", "chosen"); err == nil {
		t.Fatal("inactive account retained model permission")
	}
	member.Active = true
	center.identities["member"] = member
	center.manager = "other"
	if _, err = resolver(ctx, "member", "chosen"); err == nil {
		t.Fatal("binding change silently reused previous manager model")
	}
	center.manager = "manager"
	model["enabled"] = false
	save()
	if _, err = resolver(ctx, "member", "chosen"); err == nil {
		t.Fatal("disabled selected model silently fell back")
	}
	model["enabled"] = true
	save()
	if err = catalog.PutConfig(ctx, "manager", map[string]any{"modelCatalog": []any{}}); err != nil {
		t.Fatal(err)
	}
	if _, err = resolver(ctx, "member", "chosen"); err == nil {
		t.Fatal("deleted selected model silently fell back")
	}
}

func TestNovelResolverBackgroundCallsKeepOriginalMemberBilling(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tracker := &yzcmusage.Tracker{Store: yzcmusage.Store{DB: db}, ReceiptDir: t.TempDir()}
	expectActiveUsageReceiptBinding(t, mock, tracker.ReceiptDir)
	attempts := 0
	client := &http.Client{Transport: tracker.Wrap(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Oneapi-Request-Id": []string{fmt.Sprintf("job-attempt-%d", attempts)}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))}, nil
	}))}
	resolver := NovelCreationTextResolver(newModelOwnerTestCenter(), func(ctx context.Context, owner, model string) (batchfactoryv11.DirectorProvider, error) {
		if owner != "manager" {
			t.Fatal("wrong credential owner", owner)
		}
		return &batchfactoryv11.OpenAICompatibleProvider{UsageOwner: owner, Endpoint: "https://newapi.yzcm.site/v1/chat/completions", APIKey: "fixture-key", Model: model, Client: client}, nil
	})
	provider, err := resolver(context.Background(), "member", "chosen")
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.(batchfactoryv11.TextModelMetadataProvider).TextModelMetadata().CatalogOwner; got != "manager" {
		t.Fatal("catalog owner changed", got)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		mock.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(fmt.Sprintf("job-attempt-%d", attempt), "member", "team-a", "founder", "novel-creation", yzcmusage.Fingerprint("fixture-key"), sqlmock.AnyArg(), 200).WillReturnResult(sqlmock.NewResult(0, 1))
		ctx := yzcmusage.WithAttribution(context.Background(), "member", "team-a", "founder")
		if result, err := provider.Complete(ctx, batchfactoryv11.TextCompletionRequest{}); err != nil || result != "ok" {
			t.Fatalf("background completion %q %v", result, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
