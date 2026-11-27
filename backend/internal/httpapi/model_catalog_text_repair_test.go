package httpapi

import (
	"context"
	"errors"
	"testing"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/novelfetchworkshop"
)

type textRepairRouteStore struct {
	*novelfetchworkshop.MemoryStore
	repaired, strict, repairCalls int
}

func (s *textRepairRouteStore) GetConfig(context.Context, string) (map[string]any, error) {
	s.strict++
	return nil, errors.New("legacy cleanup retained")
}
func (*textRepairRouteStore) ReadRuntimeConfig(context.Context, string) (map[string]any, error) {
	return map[string]any{"modelCatalog": []any{map[string]any{"id": "text-main", "kind": "text", "modelId": "text-model", "displayName": "Text fixture", "baseUrl": "https://text.invalid/v1", "credentialStatus": "needs_reentry", "enabled": true}}}, nil
}
func (s *textRepairRouteStore) TryPatchBoundTextCredential(_ context.Context, owner, id string, patch map[string]any) (map[string]any, bool, error) {
	s.repairCalls++
	if owner != "founder" || id != "text-main" {
		return nil, true, errors.New("wrong owner or model")
	}
	s.repaired++
	return map[string]any{"id": id, "kind": "text", "credential": "fixture-only", "enabled": true}, true, nil
}

type restrictedTextRepairStore struct {
	*textRepairRouteStore
	approved bool
	revision any
}

func (s *restrictedTextRepairStore) ReadRuntimeConfig(context.Context, string) (map[string]any, error) {
	row := map[string]any{"id": "yzcm:text:fixture", "kind": "text", "modelId": "fixture", "provider": "custom", "baseUrl": "https://custom.invalid/v1", "catalogSource": "custom"}
	if s.approved {
		row["provider"] = "yzcm"
		row["baseUrl"] = "http://newapi.yzcm.site/v1"
		row["catalogSource"] = "yzcm_saved"
	}
	return map[string]any{"configRevision": "7", "modelCatalog": []any{row}}, nil
}
func (s *restrictedTextRepairStore) TryPatchBoundTextCredential(_ context.Context, owner, id string, patch map[string]any) (map[string]any, bool, error) {
	s.repairCalls++
	s.revision = patch[novelfetchworkshop.ConfigRevisionKey]
	return map[string]any{"id": id, "kind": "text", "modelId": "fixture", "provider": "yzcm", "baseUrl": "http://newapi.yzcm.site/v1", "catalogSource": "yzcm_saved", "credential": "fixture-only"}, true, nil
}
func TestRestrictedCredentialRepairRequiresActualManagedRowAndOriginalFields(t *testing.T) {
	for _, tc := range []struct {
		approved      bool
		payload       string
		status, calls int
	}{
		{false, `{"credential":"fixture-only"}`, 403, 0},
		{true, `{"credential":"fixture-only","enabled":true}`, 403, 0},
		{true, `{"credential":"fixture-only","displayName":"different"}`, 403, 0},
		{true, `{"credential":"fixture-only"}`, 200, 1},
	} {
		s := &restrictedTextRepairStore{textRepairRouteStore: &textRepairRouteStore{MemoryStore: novelfetchworkshop.NewMemoryStore()}, approved: tc.approved}
		h := NewRouter(RouterOptions{AccountCenterStore: newModelOwnerTestCenter(), NovelFetchStore: s, SessionResolver: func(context.Context, string) (BridgeIdentity, error) { return BridgeIdentity{Username: "manager"}, nil }})
		response := impersonationRequest(h, "PATCH", "/api/config/models/yzcm:text:fixture", "manager", tc.payload, nil)
		if response.Code != tc.status || s.repairCalls != tc.calls {
			t.Fatalf("restricted repair authorization changed: status=%d calls=%d", response.Code, s.repairCalls)
		}
		if tc.calls == 1 && s.revision != "7" {
			t.Fatal("managed row authorization not bound to storage CAS")
		}
	}
}
func TestModelManagementReadsMissingTextKeyAndRepairsActualFrontendPayload(t *testing.T) {
	s := &textRepairRouteStore{MemoryStore: novelfetchworkshop.NewMemoryStore()}
	center := newModelOwnerTestCenter()
	center.identities["founder"] = accountcenter.Identity{Username: "founder", Role: accountcenter.RoleFounder, Active: true}
	h := NewRouter(RouterOptions{AccountCenterStore: center, NovelFetchStore: s, SessionResolver: func(context.Context, string) (BridgeIdentity, error) {
		return BridgeIdentity{Username: "founder", IsOwner: true}, nil
	}})
	get := impersonationRequest(h, "GET", "/api/config/models?kind=text", "founder", "", nil)
	if get.Code != 200 {
		t.Fatalf("read-only model GET blocked: %d", get.Code)
	}
	for _, payload := range []string{`{"credential":"new-fixture","enabled":true}`, `{"displayName":"Text fixture","name":"Text fixture","modelId":"text-model","baseUrl":"https://text.invalid/v1","credential":"new-fixture"}`, `{"clearCredential":true}`} {
		response := impersonationRequest(h, "PATCH", "/api/config/models/text-main", "founder", payload, nil)
		if response.Code != 200 {
			t.Fatalf("frontend credential-only/unchanged echo payload blocked: %d", response.Code)
		}
	}
	if s.repaired != 3 || s.strict != 0 {
		t.Fatal("repair used strict legacy cleanup")
	}
}
