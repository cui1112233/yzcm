package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/storage"
	"qiantie/backend/internal/yzcmusage"
)

func TestAccountCenterEnergyRoutesRequireAuthentication(t *testing.T) {
	api := NewRouter(RouterOptions{})
	for _, endpoint := range []string{"overview", "energy", "energy/ledger", "energy/ranking", "energy/teams"} {
		response := impersonationRequest(api, http.MethodGet, "/api/account-center/"+endpoint, "", "", nil)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s must require authentication, got %d", endpoint, response.Code)
		}
	}
}

func TestAccountCenterEnergyMySQLScopesAndPrecision(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	migrations := append(storage.AuthMigrations(), storage.AccountCenterMigrations()...)
	migrations = append(migrations, storage.YZCMUsageMigrations()...)
	migrations = append(migrations, storage.AccountCenterEnergyMigrations()...)
	if err := storage.RunMigrations(ctx, db, migrations); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for name, role := range map[string]string{"founder": "founder", "developer": "developer", "lead-a": "manager", "lead-b": "manager", "member-a": "member", "member-b": "member", "transferred": "member"} {
		exec(`INSERT INTO accounts(username,password_hash,password_algorithm,is_owner,active,created_at,updated_at) VALUES (?,'fixture','fixture',?,TRUE,NOW(6),NOW(6))`, name, role == "founder")
		exec(`INSERT INTO account_center_profiles(username,display_name,role) VALUES (?,?,?)`, name, name, role)
	}
	directory := accountcenter.NewMySQLStore(db)
	for _, group := range []string{"a", "b"} {
		if err := directory.CreateTeam(ctx, accountcenter.Team{ID: "team-" + group, Name: "team-" + group, CreatedBy: "founder", ManagerUsername: "lead-" + group}); err != nil {
			t.Fatal(err)
		}
	}
	for name, team := range map[string]string{"founder": "team-a", "developer": "team-a", "member-a": "team-a", "member-b": "team-b", "transferred": "team-a"} {
		if err := directory.AddMembership(ctx, accountcenter.Membership{Username: name, TeamID: team}); err != nil {
			t.Fatal(err)
		}
	}
	const at = "2026-11-06 08:00:00"
	for _, row := range []struct {
		id, owner, team, model, at string
		cost, settled              any
	}{
		{"own-charge", "member-a", "team-a", "=fixture", at, "12345678901234.1234567890", at},
		{"other-charge", "transferred", "team-a", "fixture", at, "0.1000000001", at},
		{"other-pending", "lead-a", "team-a", "pending", at, "999.0000000000", nil},
		{"own-pending", "member-a", "team-a", "pending", at, nil, nil},
		{"foreign-charge", "member-b", "team-b", "fixture", at, "2.0000000000", at},
		{"outside-window", "member-a", "team-a", "fixture", "2026-10-05 00:00:00", "500.0000000000", at},
	} {
		exec(`INSERT INTO yzcm_usage_requests(request_id,owner,team_id,actor_username,model,feature,credential_hash,created_at,http_status,actual_cost,cost_currency,reconciled_at)
VALUES(?,?,?,'founder',?,'fixture',?, ?,200,?,'CNY',?)`, row.id, row.owner, row.team, row.model, strings.Repeat("0", 64), row.at, row.cost, row.settled)
	}
	usage := yzcmusage.Store{DB: db}
	if err := usage.Record(ctx, yzcmusage.Request{ID: "own-charge", Owner: "member-a", TeamID: "team-a", ActorUsername: "founder", Feature: "fixture"}, strings.Repeat("0", 64), 200); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE account_center_memberships SET ended_at=NOW(6) WHERE active_username='transferred'`)
	if err := directory.AddMembership(ctx, accountcenter.Membership{Username: "transferred", TeamID: "team-b"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	api := NewRouter(RouterOptions{AccountCenterStore: directory, Now: func() time.Time { return now }, SessionResolver: func(_ context.Context, token string) (BridgeIdentity, error) {
		return BridgeIdentity{Username: token, IsOwner: token == "founder"}, nil
	}})
	get := func(user, suffix string, cookie *http.Cookie, want int) string {
		t.Helper()
		response := impersonationRequest(api, http.MethodGet, "/api/account-center/"+suffix, user, "", cookie)
		if response.Code != want {
			t.Fatalf("%s %s response %d, want %d: %s", user, suffix, response.Code, want, response.Body)
		}
		return response.Body.String()
	}
	for _, role := range []string{"founder", "developer", "lead-a", "member-a"} {
		body := get(role, "overview", nil, http.StatusOK)
		if !containsAll(body, `"username":"`+role+`"`, `"id":"team-a"`, `"available":false`, `"amount":null`, `"actualCost":"12345678901234.2234567891"`, `"pendingCount":2`) {
			t.Fatalf("overview lost team or usage information: %s", body)
		}
		body = get(role, "energy/ranking", nil, http.StatusOK)
		if !containsAll(body, `"username":"member-a"`, `"username":"transferred"`, `"actualCost":"0.1000000001"`) || strings.Contains(body, "member-b") {
			t.Fatalf("ranking changed historical team or leaked another team: %s", body)
		}
	}
	for _, privileged := range []string{"founder", "developer"} {
		body := get(privileged, "energy/teams", nil, http.StatusOK)
		var page struct{ Teams []json.RawMessage }
		if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Teams) != 2 || strings.Count(body, `"balance"`) != 2 || strings.Contains(body, "combinedBalance") {
			t.Fatalf("expected two independent team wallets: %s (%v)", body, err)
		}
		if body := get(privileged, "energy?teamId=team-b", nil, http.StatusOK); !containsAll(body, `"id":"team-b"`, `"actualCost":"2.0000000000"`) {
			t.Fatalf("missing authorized other team data: %s", body)
		}
	}
	for _, scoped := range []string{"member-a", "lead-a"} {
		unknown := get(scoped, "energy?teamId=does-not-exist", nil, http.StatusForbidden)
		if other := get(scoped, "energy?teamId=team-b", nil, http.StatusForbidden); other != unknown {
			t.Fatalf("scoped denials disclosed team existence: %q vs %q", other, unknown)
		}
		get(scoped, "energy/teams", nil, http.StatusForbidden)
	}
	memberLedger := get("member-a", "energy/ledger", nil, http.StatusOK)
	if !containsAll(memberLedger, `"requestId":"own-charge"`, `"requestId":"own-pending"`, `"actualCost":null`) ||
		strings.Contains(memberLedger, "other-charge") || strings.Contains(memberLedger, "actorUsername") || strings.Contains(memberLedger, "outside-window") {
		t.Fatalf("member ledger did not restrict detail to the subject: %s", memberLedger)
	}
	get("member-a", "energy/ledger?member=transferred", nil, http.StatusForbidden)
	get("member-a", "energy?member=transferred", nil, http.StatusForbidden)
	get("transferred", "energy/ledger?teamId=team-a", nil, http.StatusForbidden)
	if own := get("transferred", "energy/ledger", nil, http.StatusOK); !strings.Contains(own, `"entries":[]`) || strings.Contains(own, "other-charge") {
		t.Fatalf("historical receipts moved into the subject's new team: %s", own)
	}
	if foreign := get("lead-a", "energy/ledger?member=member-b", nil, http.StatusForbidden); foreign != get("lead-a", "energy/ledger?member=no-such-member", nil, http.StatusForbidden) {
		t.Fatal("manager's scoped member denial disclosed a foreign username")
	}
	managerLedger := get("lead-a", "energy/ledger?member=transferred", nil, http.StatusOK)
	if !containsAll(managerLedger, `"requestId":"other-charge"`, `"actorUsername":"founder"`) || strings.Contains(managerLedger, "own-charge") {
		t.Fatalf("manager ledger did not filter the team's original attribution: %s", managerLedger)
	}
	if page := get("lead-a", "energy/ledger?limit=1", nil, http.StatusOK); !containsAll(page, `"nextOffset":1`) || strings.Count(page, `"requestId"`) != 1 {
		t.Fatalf("ledger pagination not bounded: %s", page)
	}
	if export := get("lead-a", "energy/ledger?format=csv&member=member-a&from=2026-11-06&to=2026-11-07", nil, http.StatusOK); !strings.Contains(export, "'=fixture") || strings.Contains(export, "other-charge") {
		t.Fatalf("CSV export changed scope or allowed a formula: %s", export)
	}
	const csvPagePath = "/api/account-center/energy/ledger?format=csv&limit=1"
	csvPage := impersonationRequest(api, http.MethodGet, csvPagePath, "member-a", "", nil)
	if csvPage.Code != http.StatusOK || csvPage.Header().Get("X-Energy-Page-Limit") != "1" || csvPage.Header().Get("X-Energy-Has-More") != "true" ||
		csvPage.Header().Get("X-Energy-Next-Offset") != "1" || csvPage.Header().Get("Content-Disposition") != `attachment; filename="energy-ledger-page.csv"` ||
		strings.Count(csvPage.Body.String(), "\n") != 2 || strings.Contains(csvPage.Body.String(), "actorUsername") {
		t.Fatalf("CSV page leaked the actor or lacked accurate pagination: %+v %s", csvPage.Header(), csvPage.Body)
	}
	const withoutReceipts = "?from=2026-11-06&to=2026-11-06T08%3A00%3A00Z&member=member-a"
	if body := get("lead-a", "energy"+withoutReceipts, nil, http.StatusOK); !strings.Contains(body, `"requestCount":0`) {
		t.Fatalf("summary included a receipt at the exclusive upper bound: %s", body)
	}
	for _, endpoint := range []string{"energy/ledger", "energy/ranking"} {
		if body := get("lead-a", endpoint+withoutReceipts, nil, http.StatusOK); !strings.Contains(body, `"entries":[]`) {
			t.Fatalf("%s disagreed with the summary time filter: %s", endpoint, body)
		}
	}
	get("lead-a", "energy/ledger?limit=101", nil, http.StatusBadRequest)
	get("lead-a", "energy/ranking?from=2025-11-01&to=2026-12-01", nil, http.StatusBadRequest)
	cookie := startImpersonationForTest(t, api, "founder", "member-a")
	if switched := get("founder", "energy/ledger", cookie, http.StatusOK); switched != memberLedger {
		t.Fatalf("switched ledger did not use subject scope: %s versus %s", switched, memberLedger)
	}
	get("founder", "energy/teams", cookie, http.StatusForbidden)
	exec(`UPDATE account_center_teams SET archived_at=NOW(6) WHERE id='team-a'`)
	get("member-a", "energy?teamId=team-a", nil, http.StatusForbidden)
	if historic := get("founder", "energy?teamId=team-a", nil, http.StatusOK); !strings.Contains(historic, `"actualCost":"12345678901234.2234567891"`) {
		t.Fatalf("archiving removed authorized historical usage: %s", historic)
	}
}
