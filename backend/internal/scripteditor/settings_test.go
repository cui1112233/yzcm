package scripteditor

import "testing"

func TestResolveSettingsReportsInheritanceSource(t *testing.T) {
	system := DefaultWorkSettings()
	account := WorkSettings{ModelID: "account-model"}
	work := WorkSettings{Ratio: "16:9"}
	resolved := ResolveSettings(system, account, work)
	if resolved.Sources["ratio"] != "work" || resolved.Sources["modelId"] != "account" {
		t.Fatalf("resolved=%#v", resolved)
	}
}
