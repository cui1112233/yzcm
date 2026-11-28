package httpapi

import (
	"context"
	"qiantie/backend/internal/novelfetchworkshop"
	"testing"
)

func TestWebSubmitUsesTaskPlatformOverProfile(t *testing.T) {
	for _, tc := range []struct {
		name string
		meta map[string]any
		want string
	}{
		{"current", map[string]any{"platformId": "2", "processSnapshot": map[string]any{"platform_id": "6"}}, "2"},
		{"snake", map[string]any{"platform_id": "2"}, "2"},
		{"recovered snapshot", map[string]any{"processSnapshot": map[string]any{"platform_id": "2"}}, "2"},
		{"legacy profile fallback", map[string]any{}, "15"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := novelfetchworkshop.NewMemoryStore()
			profileConfig := map[string]any{"id": "layout", "platform_id": "15", "advanced": map[string]any{"ziti": 4}}
			webConfig := map[string]any{"enabled": true, "min_file_size": 0, "organization": "org-1", "selected_profile": "layout", "upload_profiles": []any{profileConfig}}
			config := map[string]any{"appConfig": map[string]any{"web_submit": webConfig}}
			if err := store.PutConfig(ctx, "tester", config); err != nil {
				t.Fatal(err)
			}
			tc.meta["style"] = "年代通用"
			doc := novelfetchworkshop.Document{BookID: "7705784637234105369", Gender: "女频", Meta: tc.meta}
			if err := store.PutDocument(ctx, "tester", doc); err != nil {
				t.Fatal(err)
			}
			if _, err := store.PutBody(ctx, "tester", novelfetchworkshop.BodyRecord{BookID: doc.BookID, BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai2", State: "ready"}, Content: "她推开旧木门，看见那个熟悉的身影。"}); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := saveUploadTargetSession(dir, "tester", map[string]any{"cookie": "fixture"}); err != nil {
				t.Fatal(err)
			}
			connector := &configuredSubmitConnector{}
			_, err := executeWebSubmit(ctx, "tester", dir, store, webSubmitRequest{TaskIDs: []string{doc.BookID}, Versions: []string{"ai2"}}, nil, connector)
			if err != nil {
				t.Fatal(err)
			}
			fields := connector.lastInput.FormFields
			if connector.submitCalls != 1 || fields["platform_id"] != tc.want || fields["ziti"] != "4" {
				t.Fatalf("task platform/layout mismatch calls=%d fields=%#v", connector.submitCalls, fields)
			}
			saved, err := store.GetConfig(ctx, "tester")
			if err != nil {
				t.Fatal(err)
			}
			profile := webSubmitProfile(mapOrEmpty(mapOrEmpty(saved["appConfig"])["web_submit"]), "ai2")
			if profile["platform_id"] != "15" {
				t.Fatal("shared profile mutated")
			}
		})
	}
}
