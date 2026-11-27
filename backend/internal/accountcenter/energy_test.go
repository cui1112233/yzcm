package accountcenter

import (
	"context"
	"errors"
	"testing"
	"time"
)

type energyWalletFixture func(context.Context, string) (EnergyWalletReading, error)

func (f energyWalletFixture) ReadTeamBalance(ctx context.Context, id string) (EnergyWalletReading, error) {
	return f(ctx, id)
}

func TestEnergyWalletReportsAuthoritativeAmountOrAvailability(t *testing.T) {
	updated := time.Date(2026, 11, 3, 12, 0, 0, 0, energyZone)
	for _, test := range []struct {
		name, reason string
		reading      EnergyWalletReading
		err          error
	}{
		{"unavailable", "wallet_unavailable", EnergyWalletReading{}, nil},
		{"error", "wallet_error", EnergyWalletReading{Amount: "12.00", Available: true}, errors.New("adapter fixture error")},
		{"stale", "wallet_stale", EnergyWalletReading{Amount: "12.00", Available: true, Stale: true, UpdatedAt: &updated}, nil},
		{"invalid", "wallet_error", EnergyWalletReading{Amount: "1e20", Available: true}, nil},
		{"zero", "", EnergyWalletReading{Amount: "0.00", Available: true, UpdatedAt: &updated}, nil},
		{"fresh", "", EnergyWalletReading{Amount: "12345678901234.1234567890", Available: true}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := EnergyService{wallet: energyWalletFixture(func(_ context.Context, id string) (EnergyWalletReading, error) {
				if id != "team-a" {
					t.Fatalf("wallet read was not scoped to the team: %s", id)
				}
				return test.reading, test.err
			})}
			value := s.balance(context.Background(), "team-a")
			if value.Available != (test.reason == "") || value.Reason != test.reason || value.Unit != EnergyUnit {
				t.Fatalf("unexpected availability: %+v", value)
			}
			if value.Available && (value.Amount == nil || *value.Amount != test.reading.Amount) || !value.Available && value.Amount != nil {
				t.Fatalf("unexpected amount: %+v", value)
			}
			if test.reading.UpdatedAt != nil && (value.UpdatedAt == nil || !value.UpdatedAt.Equal(updated) || value.UpdatedAt.Location() != time.UTC) {
				t.Fatalf("wallet timestamp lost: %+v", value)
			}
		})
	}
}

func TestEnergyFilterNormalizesCalendarAndBoundsPages(t *testing.T) {
	s := EnergyService{now: func() time.Time { return time.Date(2026, 11, 30, 17, 0, 0, 0, time.UTC) }}
	filter, page, err := s.normalize(EnergyQuery{})
	if err != nil || filter.From.Format(time.RFC3339) != "2026-11-30T16:00:00Z" || filter.To.Format(time.RFC3339) != "2026-12-31T16:00:00Z" || page.Limit != 50 {
		t.Fatalf("default filter=%+v page=%+v error=%v", filter, page, err)
	}
	filter, _, err = s.normalize(EnergyQuery{From: "2026-11-06", To: "2026-11-07T00:00:00+08:00"})
	if err != nil || filter.From.Format(time.RFC3339) != "2026-11-05T16:00:00Z" || filter.To.Sub(filter.From) != 24*time.Hour {
		t.Fatalf("explicit filter=%+v error=%v", filter, err)
	}
	for _, query := range []EnergyQuery{{Offset: 10001}, {Limit: 101}, {From: "2022-01-01", To: "2026-01-01"}, {From: "2026-11-07", To: "2026-11-06"}} {
		if _, _, err := s.normalize(query); !errors.Is(err, ErrInvalid) {
			t.Errorf("unbounded or unordered query accepted: %+v %v", query, err)
		}
	}
}
