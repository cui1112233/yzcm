package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	for _, name := range []string{"peer-a", "equal-a", "zero-a", "negative-a", "inactive-a"} {
		exec(`INSERT INTO accounts(username,password_hash,password_algorithm,is_owner,active,created_at,updated_at) VALUES (?,'fixture','fixture',FALSE,?,NOW(6),NOW(6))`, name, name != "inactive-a")
		exec(`INSERT INTO account_center_profiles(username,display_name,role) VALUES (?,?,'member')`, name, name)
	}
	directory := accountcenter.NewMySQLStore(db)
	for _, group := range []string{"a", "b"} {
		if err := directory.CreateTeam(ctx, accountcenter.Team{ID: "team-" + group, Name: "team-" + group, CreatedBy: "founder", ManagerUsername: "lead-" + group}); err != nil {
			t.Fatal(err)
		}
	}
	for name, team := range map[string]string{"founder": "team-a", "developer": "team-a", "member-a": "team-a", "member-b": "team-b", "transferred": "team-a", "peer-a": "team-a", "equal-a": "team-a", "zero-a": "team-a", "negative-a": "team-a", "inactive-a": "team-a"} {
		if err := directory.AddMembership(ctx, accountcenter.Membership{Username: name, TeamID: team}); err != nil {
			t.Fatal(err)
		}
	}
	const at = "2026-11-06 08:00:00"
	const peerAt = "2026-12-07 08:00:00"
	const rankAt = "2026-12-08 08:00:00"
	const pendingAt = "2026-12-09 08:00:00"
	const compatibilityModel = " \uFEFF　⁺\u0301样本,\"原文\""
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
		{"peer-aggregate", "peer-a", "team-a", compatibilityModel, peerAt, "1.2500000000", peerAt},
		{"peer-rank", "peer-a", "team-a", "fixture", rankAt, "0.5000000001", rankAt},
		{"peer-negative-anomaly", "peer-a", "team-a", "fixture", rankAt, "-2.0000000000", rankAt},
		{"peer-pending", "peer-a", "team-a", "fixture", rankAt, nil, nil},
		{"peer-missing-amount", "peer-a", "team-a", "fixture", rankAt, nil, rankAt},
		{"equal-rank", "equal-a", "team-a", "fixture", rankAt, "0.5000000001", rankAt},
		{"zero-rank", "zero-a", "team-a", "fixture", rankAt, "0.0000000000", rankAt},
		{"negative-only", "negative-a", "team-a", "fixture", rankAt, "-1.0000000000", rankAt},
		{"pending-with-ranked", "lead-a", "team-a", "fixture", rankAt, "500.0000000000", nil},
		{"pending-without-ranked", "lead-a", "team-a", "fixture", pendingAt, nil, nil},
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
	for _, resource := range []string{"overview", "energy", "energy/ranking"} {
		path := "/api/account-center/" + resource
		peer := impersonationRequest(api, http.MethodGet, path+"?member=peer-a&from=2026-12-07&to=2026-12-08", "member-a", "", nil)
		if peer.Code != http.StatusOK || !strings.Contains(peer.Body.String(), `"actualCost":"1.2500000000"`) || strings.Contains(peer.Body.String(), "actorUsername") {
			t.Errorf("%s did not authorize a current peer's aggregate: %d %s", resource, peer.Code, peer.Body)
		}
		unknown := get("member-a", resource+"?member=no-such-member", nil, http.StatusForbidden)
		for _, target := range []string{"member-b", "transferred", "inactive-a"} {
			denial := impersonationRequest(api, http.MethodGet, path+"?member="+target, "member-a", "", nil)
			if denial.Code != http.StatusForbidden || denial.Body.String() != unknown {
				t.Errorf("%s disclosed cross-team, former, or inactive member %s: %d %s", resource, target, denial.Code, denial.Body)
			}
		}
	}
	get("member-a", "energy/ledger?member=peer-a", nil, http.StatusForbidden)
	get("member-a", "energy/ledger?format=csv&member=peer-a", nil, http.StatusForbidden)
	const rankingWindow = "energy/ranking?from=2026-12-08&to=2026-12-09"
	for _, page := range []struct {
		offset       string
		user, amount string
		rank         int
		pending      int64
	}{{"0", "equal-a", "0.5000000001", 1, 0}, {"1", "peer-a", "0.5000000001", 1, 2}, {"2", "zero-a", "0.0000000000", 3, 0}} {
		var result struct {
			Entries []accountcenter.EnergyRankingEntry
		}
		body := get("member-a", rankingWindow+"&limit=1&offset="+page.offset, nil, http.StatusOK)
		if err := json.Unmarshal([]byte(body), &result); err != nil || len(result.Entries) != 1 {
			t.Errorf("unexpected ranking page: %s (%v)", body, err)
			continue
		}
		entry := result.Entries[0]
		if entry.Username != page.user || entry.Rank != page.rank || entry.ActualCost != page.amount || entry.ReconciledCount != 1 || entry.PendingCount != page.pending {
			t.Errorf("ranking did not preserve ties, legitimate zero, or filter anomalous charges: %s", body)
		}
	}
	if body := get("member-a", rankingWindow+"&offset=3", nil, http.StatusOK); !containsAll(body, `"entries":[]`, `"emptyReason":"page_out_of_range"`) {
		t.Errorf("pending-only or negative-only members polluted the ranking: %s", body)
	}
	if body := get("lead-a", "energy/ledger?from=2026-12-08&to=2026-12-09&member=negative-a", nil, http.StatusOK); !containsAll(body, `"actualCost":"-1.0000000000"`) {
		t.Errorf("ranking cleanup changed the ledger's retained amount: %s", body)
	}
	const pendingWindow = "?from=2026-12-09&to=2026-12-10"
	if body := get("member-a", "energy/ranking"+pendingWindow, nil, http.StatusOK); !containsAll(body, `"entries":[]`, `"emptyReason":"no_reconciled_consumption"`) {
		t.Errorf("a pending-only window received a consumption ranking: %s", body)
	}
	if body := get("member-a", "energy"+pendingWindow, nil, http.StatusOK); !containsAll(body, `"pendingCount":1`, `"reconciledCount":0`) {
		t.Errorf("pending requests disappeared from the team summary: %s", body)
	}
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
	compatibilityCSV := get("lead-a", "energy/ledger?format=csv&member=peer-a&from=2026-12-07&to=2026-12-08", nil, http.StatusOK)
	records, err := csv.NewReader(strings.NewReader(compatibilityCSV)).ReadAll()
	if err != nil || len(records) != 2 || len(records[1]) != 8 || records[1][4] != "'"+compatibilityModel || records[1][6] != "1.2500000000" {
		t.Errorf("HTTP CSV did not preserve and protect the model text or decimal: %q (%v)", records, err)
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
	get("founder", "energy?member=transferred", cookie, http.StatusForbidden)
	exec(`UPDATE account_center_teams SET archived_at=NOW(6) WHERE id='team-a'`)
	get("member-a", "energy?teamId=team-a", nil, http.StatusForbidden)
	if historic := get("founder", "energy?teamId=team-a", nil, http.StatusOK); !strings.Contains(historic, `"actualCost":"12345678901234.2234567891"`) {
		t.Fatalf("archiving removed authorized historical usage: %s", historic)
	}
}

func TestAccountCenterEnergyCSVRoundTripProtectsEveryTextColumn(t *testing.T) {
	numeric := "-0.1250000000"
	for _, test := range []struct {
		input  string
		escape bool
	}{
		{"=样本", true}, {"+样本", true}, {"-样本", true}, {"@样本", true},
		{"＝样本", true}, {"＋样本", true}, {"－样本", true}, {"＠样本", true},
		{"﹦样本", true}, {"﹢样本", true}, {"﹣样本", true}, {"﹫样本", true}, {"−样本", true},
		{"⁺", true}, {"⁼", true}, {"₊", true}, {"₌", true}, {"﬩", true},
		{"  \ufeff　＠样本,\"引号\"", true}, {"  \t样本", true}, {" \ufeff\r样本", true}, {" \n样本", true},
		{" \ufeff\u2003\u00a0₌\u0338样本", true}, {"=\u0338样本", true}, {strings.Repeat("　", 254) + "⁺", true},
		{"普通中文,\"引号\"", false}, {"  普通中文", false},
		{"Cafe\u0301 and 中文 ①", false}, {"\ufeff\u2003", false}, {"", false},
	} {
		t.Run(test.input, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeAccountEnergyCSV(response, accountcenter.EnergyLedger{Entries: []accountcenter.EnergyLedgerEntry{{
				RequestID: test.input, Owner: test.input, Feature: test.input, Model: test.input, Status: test.input, ActorUsername: test.input, ActualCost: &numeric,
			}}}, true)
			records, err := csv.NewReader(response.Body).ReadAll()
			if err != nil || len(records) != 2 || len(records[1]) != 8 || records[1][6] != numeric || records[0][6] != "actualCost" {
				t.Fatalf("unexpected CSV shape or modified decimal: %q, %v", records, err)
			}
			want := test.input
			if test.escape {
				want = "'" + want
			}
			for _, column := range []int{0, 1, 3, 4, 5, 7} {
				if records[1][column] != want {
					t.Errorf("text column %s round-tripped as %q, want %q", records[0][column], records[1][column], want)
				}
			}
		})
	}
}
