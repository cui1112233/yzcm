package accountcenter

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"qiantie/backend/internal/yzcmusage"
)

const (
	EnergyUnit      = "energy"
	EnergyTimeZone  = "Asia/Shanghai"
	EnergyConsole   = yzcmusage.Origin + "/"
	maxEnergyLimit  = 100
	maxEnergyOffset = 10000
)

var energyZone = time.FixedZone(EnergyTimeZone, 8*60*60)
var energyDecimal = regexp.MustCompile(`^-?[0-9]{1,30}(\.[0-9]{1,10})?$`)

// EnergyQuery is shared by every read surface, including ledger export.
// Calendar dates are interpreted in Asia/Shanghai. From is inclusive and To is exclusive.
type EnergyQuery struct {
	TeamID, Member, From, To string
	Limit, Offset            int
}

type EnergyFilter struct {
	TeamID   string    `json:"teamId,omitempty"`
	Member   string    `json:"member,omitempty"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	TimeZone string    `json:"timeZone"`
}

type EnergyTeam struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	ArchivedAt *time.Time `json:"archivedAt"`
}

type EnergySubject struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	TeamID      string `json:"teamId,omitempty"`
}

type EnergyTotals struct {
	ActualCost      string `json:"actualCost"`
	Unit            string `json:"unit"`
	RequestCount    int64  `json:"requestCount"`
	ReconciledCount int64  `json:"reconciledCount"`
	PendingCount    int64  `json:"pendingCount"`
}

type EnergyBalance struct {
	Available bool       `json:"available"`
	Amount    *string    `json:"amount"`
	Unit      string     `json:"unit"`
	Reason    string     `json:"reason,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// EnergyWallet reads the authoritative YZCM wallet bound to a team and reports
// freshness according to the wallet source's own policy.
type EnergyWallet interface {
	ReadTeamBalance(context.Context, string) (EnergyWalletReading, error)
}

type EnergyWalletReading struct {
	Amount    string
	Available bool
	Stale     bool
	UpdatedAt *time.Time
}

type UnavailableEnergyWallet struct{}

func (UnavailableEnergyWallet) ReadTeamBalance(context.Context, string) (EnergyWalletReading, error) {
	return EnergyWalletReading{}, nil
}

type EnergyTeamSummary struct {
	Team        *EnergyTeam   `json:"team"`
	Consumption *EnergyTotals `json:"consumption"`
	Balance     EnergyBalance `json:"balance"`
}

type EnergySummary struct {
	EnergyTeamSummary
	Filter      EnergyFilter `json:"filter"`
	RechargeURL string       `json:"rechargeUrl"`
}

type EnergyOverview struct {
	EnergySummary
	Subject         EnergySubject `json:"subject"`
	CanViewAllTeams bool          `json:"canViewAllTeams"`
}

type EnergyLedgerEntry struct {
	RequestID     string     `json:"requestId"`
	Owner         string     `json:"owner"`
	ActorUsername string     `json:"actorUsername,omitempty"`
	At            time.Time  `json:"at"`
	Feature       string     `json:"feature"`
	Model         string     `json:"model"`
	Status        string     `json:"status"`
	HTTPStatus    int        `json:"httpStatus"`
	ActualCost    *string    `json:"actualCost"`
	ReconciledAt  *time.Time `json:"reconciledAt,omitempty"`
}

type EnergyRankingEntry struct {
	Username string `json:"username"`
	Rank     int    `json:"rank"`
	EnergyTotals
}

type EnergyPage struct {
	Limit      int  `json:"limit"`
	Offset     int  `json:"offset"`
	HasMore    bool `json:"hasMore"`
	NextOffset *int `json:"nextOffset,omitempty"`
}

type EnergyLedger struct {
	EnergyPage
	Team    EnergyTeam          `json:"team"`
	Filter  EnergyFilter        `json:"filter"`
	Unit    string              `json:"unit"`
	Entries []EnergyLedgerEntry `json:"entries"`
}

type EnergyRanking struct {
	EnergyPage
	Team        EnergyTeam           `json:"team"`
	Filter      EnergyFilter         `json:"filter"`
	Entries     []EnergyRankingEntry `json:"entries"`
	EmptyReason string               `json:"emptyReason,omitempty"`
}

type EnergyTeams struct {
	EnergyPage
	Filter      EnergyFilter        `json:"filter"`
	Teams       []EnergyTeamSummary `json:"teams"`
	RechargeURL string              `json:"rechargeUrl"`
}

type energyReadStore interface {
	energyTeam(context.Context, string) (EnergyTeam, error)
	energyHasMember(context.Context, EnergyFilter, bool) (bool, error)
	energyTotals(context.Context, EnergyFilter) (EnergyTotals, error)
	energyLedger(context.Context, EnergyFilter, EnergyPage, bool) ([]EnergyLedgerEntry, error)
	energyRanking(context.Context, EnergyFilter, EnergyPage) ([]EnergyRankingEntry, error)
	energyTeams(context.Context, EnergyFilter, EnergyPage) ([]EnergyTeamSummary, error)
}

type EnergyService struct {
	store  energyReadStore
	wallet EnergyWallet
	now    func() time.Time
}

type energyView uint8

const (
	energySummaryView energyView = iota
	energyLedgerView
)

func NewEnergyService(store Store, wallet EnergyWallet, now func() time.Time) *EnergyService {
	reader, ok := store.(energyReadStore)
	if !ok {
		return nil
	}
	if wallet == nil {
		wallet = UnavailableEnergyWallet{}
	}
	if now == nil {
		now = time.Now
	}
	return &EnergyService{store: reader, wallet: wallet, now: now}
}

func (s *EnergyService) Overview(ctx context.Context, subject Identity, query EnergyQuery) (EnergyOverview, error) {
	var result EnergyOverview
	if !energySubjectAllowed(subject) {
		return result, ErrForbidden
	}
	result.Subject = EnergySubject{Username: subject.Username, DisplayName: subject.DisplayName, Role: subject.Role, TeamID: subject.TeamID}
	result.CanViewAllTeams = energyAllTeams(subject)
	if subject.TeamID == "" && strings.TrimSpace(query.TeamID) == "" && strings.TrimSpace(query.Member) == "" {
		filter, _, err := s.normalize(query)
		result.EnergySummary = EnergySummary{Filter: filter, RechargeURL: EnergyConsole, EnergyTeamSummary: EnergyTeamSummary{Balance: EnergyBalance{Unit: EnergyUnit, Reason: "no_active_team"}}}
		return result, err
	}
	var err error
	result.EnergySummary, err = s.Energy(ctx, subject, query)
	return result, err
}

func (s *EnergyService) Energy(ctx context.Context, subject Identity, query EnergyQuery) (EnergySummary, error) {
	filter, _, team, err := s.scope(ctx, subject, query, energySummaryView)
	if err != nil {
		return EnergySummary{}, err
	}
	totals, err := s.store.energyTotals(ctx, filter)
	if err != nil {
		return EnergySummary{}, err
	}
	return EnergySummary{
		EnergyTeamSummary: EnergyTeamSummary{Team: &team, Consumption: &totals, Balance: s.balance(ctx, team.ID)},
		Filter:            filter, RechargeURL: EnergyConsole,
	}, nil
}

func (s *EnergyService) Ledger(ctx context.Context, subject Identity, query EnergyQuery) (EnergyLedger, error) {
	filter, page, team, err := s.scope(ctx, subject, query, energyLedgerView)
	if err != nil {
		return EnergyLedger{}, err
	}
	entries, err := s.store.energyLedger(ctx, filter, page, subject.Role != RoleMember)
	if err != nil {
		return EnergyLedger{}, err
	}
	return EnergyLedger{EnergyPage: page.withMore(len(entries)), Team: team, Filter: filter, Unit: EnergyUnit, Entries: entries[:min(len(entries), page.Limit)]}, nil
}

func (s *EnergyService) Ranking(ctx context.Context, subject Identity, query EnergyQuery) (EnergyRanking, error) {
	filter, page, team, err := s.scope(ctx, subject, query, energySummaryView)
	if err != nil {
		return EnergyRanking{}, err
	}
	entries, err := s.store.energyRanking(ctx, filter, page)
	if err != nil {
		return EnergyRanking{}, err
	}
	result := EnergyRanking{EnergyPage: page.withMore(len(entries)), Team: team, Filter: filter, Entries: entries[:min(len(entries), page.Limit)]}
	if len(entries) == 0 {
		result.EmptyReason = "no_reconciled_consumption"
		if page.Offset != 0 {
			result.EmptyReason = "page_out_of_range"
		}
	}
	return result, nil
}

func (s *EnergyService) Teams(ctx context.Context, subject Identity, query EnergyQuery) (EnergyTeams, error) {
	if !energySubjectAllowed(subject) || !energyAllTeams(subject) {
		return EnergyTeams{}, ErrForbidden
	}
	filter, page, err := s.normalize(query)
	if err != nil {
		return EnergyTeams{}, err
	}
	if filter.TeamID != "" {
		filter, page, _, err = s.scope(ctx, subject, query, energySummaryView)
	} else if filter.Member != "" {
		err = s.authorizeMember(ctx, filter, false)
	}
	if err != nil {
		return EnergyTeams{}, err
	}
	teams, err := s.store.energyTeams(ctx, filter, page)
	if err != nil {
		return EnergyTeams{}, err
	}
	result := EnergyTeams{EnergyPage: page.withMore(len(teams)), Filter: filter, Teams: teams[:min(len(teams), page.Limit)], RechargeURL: EnergyConsole}
	for i := range result.Teams {
		result.Teams[i].Balance = s.balance(ctx, result.Teams[i].Team.ID)
	}
	return result, nil
}

func (s *EnergyService) scope(ctx context.Context, subject Identity, query EnergyQuery, view energyView) (EnergyFilter, EnergyPage, EnergyTeam, error) {
	var team EnergyTeam
	if !energySubjectAllowed(subject) {
		return EnergyFilter{}, EnergyPage{}, team, ErrForbidden
	}
	filter, page, err := s.normalize(query)
	if err != nil {
		return filter, page, team, err
	}
	if filter.TeamID == "" {
		filter.TeamID = subject.TeamID
	}
	if filter.TeamID == "" || !energyAllTeams(subject) && filter.TeamID != subject.TeamID {
		return filter, page, team, ErrForbidden
	}
	if subject.Role == RoleMember && view == energyLedgerView {
		if filter.Member != "" && !strings.EqualFold(filter.Member, subject.Username) {
			return filter, page, team, ErrForbidden
		}
		filter.Member = subject.Username
	}
	team, err = s.store.energyTeam(ctx, filter.TeamID)
	if err != nil {
		return filter, page, team, err
	}
	if !energyAllTeams(subject) && team.ArchivedAt != nil {
		return filter, page, team, ErrForbidden
	}
	filter.TeamID = team.ID
	return filter, page, team, s.authorizeMember(ctx, filter, subject.Role == RoleMember)
}

func (s *EnergyService) authorizeMember(ctx context.Context, filter EnergyFilter, currentOnly bool) error {
	if filter.Member == "" {
		return nil
	}
	allowed, err := s.store.energyHasMember(ctx, filter, currentOnly)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func energySubjectAllowed(subject Identity) bool {
	return subject.Username != "" && subject.Active && subject.ArchivedAt == nil &&
		(energyAllTeams(subject) || subject.Role == RoleManager || subject.Role == RoleMember)
}

func energyAllTeams(subject Identity) bool {
	return subject.Role == RoleFounder || subject.Role == RoleDeveloper
}

func (s *EnergyService) normalize(query EnergyQuery) (EnergyFilter, EnergyPage, error) {
	filter := EnergyFilter{TeamID: strings.TrimSpace(query.TeamID), Member: strings.TrimSpace(query.Member), TimeZone: EnergyTimeZone}
	page := EnergyPage{Limit: query.Limit, Offset: query.Offset}
	if len(filter.TeamID) > 64 || len(filter.Member) > 32 {
		return filter, page, ErrForbidden
	}
	if page.Limit == 0 {
		page.Limit = 50
	}
	if page.Limit < 0 || page.Limit > maxEnergyLimit || page.Offset < 0 || page.Offset > maxEnergyOffset {
		return filter, page, fmt.Errorf("%w: limit or offset outside the supported range", ErrInvalid)
	}
	now := s.now().In(energyZone)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, energyZone)
	var fromErr, toErr error
	filter.From, fromErr = parseEnergyBound(query.From, month)
	filter.To, toErr = parseEnergyBound(query.To, month.AddDate(0, 1, 0))
	if fromErr != nil || toErr != nil || filter.From.Year() < 1000 || filter.To.Year() > 9999 ||
		!filter.From.Before(filter.To) || filter.To.Sub(filter.From) > 366*24*time.Hour {
		return filter, page, fmt.Errorf("%w: invalid energy time window", ErrInvalid)
	}
	return filter, page, nil
}

func parseEnergyBound(value string, fallback time.Time) (time.Time, error) {
	if value == "" {
		return fallback.UTC(), nil
	}
	if len(value) == len(time.DateOnly) {
		date, err := time.ParseInLocation(time.DateOnly, value, energyZone)
		return date.UTC(), err
	}
	at, err := time.Parse(time.RFC3339Nano, value)
	return at.UTC(), err
}

func (p EnergyPage) withMore(readRows int) EnergyPage {
	p.HasMore = readRows > p.Limit
	if next := p.Offset + p.Limit; p.HasMore && next <= maxEnergyOffset {
		p.NextOffset = &next
	}
	return p
}

func (s *EnergyService) balance(ctx context.Context, teamID string) EnergyBalance {
	result := EnergyBalance{Unit: EnergyUnit, Reason: "wallet_unavailable"}
	reading, err := s.wallet.ReadTeamBalance(ctx, teamID)
	if reading.UpdatedAt != nil {
		at := reading.UpdatedAt.UTC()
		result.UpdatedAt = &at
	}
	switch {
	case err != nil:
		result.Reason = "wallet_error"
	case reading.Stale:
		result.Reason = "wallet_stale"
	case reading.Available && energyDecimal.MatchString(reading.Amount):
		result.Available, result.Amount, result.Reason = true, &reading.Amount, ""
	case reading.Available:
		result.Reason = "wallet_error"
	}
	return result
}
