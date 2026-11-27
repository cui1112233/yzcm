package httpapi

import (
	"encoding/csv"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"qiantie/backend/internal/accountcenter"
)

func registerAccountCenterEnergy(root *http.ServeMux, auth BridgeAuth, now func() time.Time) {
	service := accountcenter.NewEnergyService(auth.AccountCenterStore, accountcenter.UnavailableEnergyWallet{}, now)
	for _, resource := range []string{"overview", "energy", "energy/ledger", "energy/ranking", "energy/teams"} {
		root.Handle("GET /api/account-center/"+resource, auth.AccountCenterMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if service == nil {
				writeAuthError(w, http.StatusServiceUnavailable, "能量服务暂不可用")
				return
			}
			identity, ok := AccountCenterIdentityFromContext(r.Context())
			if !ok {
				writeAccountCenterForbidden(w)
				return
			}
			query, format, err := accountEnergyQuery(r.URL.RawQuery, resource == "energy/ledger")
			if err != nil {
				writeAccountEnergyResult(w, nil, err)
				return
			}
			ctx, subject := r.Context(), identity.Subject
			switch resource {
			case "overview":
				value, err := service.Overview(ctx, subject, query)
				writeAccountEnergyResult(w, value, err)
			case "energy":
				value, err := service.Energy(ctx, subject, query)
				writeAccountEnergyResult(w, value, err)
			case "energy/ledger":
				value, err := service.Ledger(ctx, subject, query)
				if err == nil && format == "csv" {
					writeAccountEnergyCSV(w, value, subject.Role != accountcenter.RoleMember)
					return
				}
				writeAccountEnergyResult(w, value, err)
			case "energy/ranking":
				value, err := service.Ranking(ctx, subject, query)
				writeAccountEnergyResult(w, value, err)
			case "energy/teams":
				value, err := service.Teams(ctx, subject, query)
				writeAccountEnergyResult(w, value, err)
			}
		})))
	}
}

func accountEnergyQuery(raw string, exportAllowed bool) (accountcenter.EnergyQuery, string, error) {
	var query accountcenter.EnergyQuery
	values, err := url.ParseQuery(raw)
	if err != nil {
		return query, "", accountcenter.ErrInvalid
	}
	for key, value := range values {
		switch key {
		case "teamId", "member", "from", "to", "limit", "offset", "format":
		default:
			return query, "", accountcenter.ErrInvalid
		}
		if len(value) != 1 || value[0] == "" {
			return query, "", accountcenter.ErrInvalid
		}
	}
	query.TeamID, query.Member = values.Get("teamId"), values.Get("member")
	query.From, query.To = values.Get("from"), values.Get("to")
	if value := values.Get("limit"); value != "" {
		if query.Limit, err = strconv.Atoi(value); err != nil || query.Limit < 1 {
			return query, "", accountcenter.ErrInvalid
		}
	}
	if value := values.Get("offset"); value != "" {
		if query.Offset, err = strconv.Atoi(value); err != nil {
			return query, "", accountcenter.ErrInvalid
		}
	}
	format := values.Get("format")
	if format != "" && (!exportAllowed || (format != "csv" && format != "json")) {
		return query, "", accountcenter.ErrInvalid
	}
	return query, format, nil
}

func writeAccountEnergyResult(w http.ResponseWriter, result any, err error) {
	switch {
	case errors.Is(err, accountcenter.ErrForbidden), errors.Is(err, accountcenter.ErrNotFound):
		writeAccountCenterForbidden(w)
	case errors.Is(err, accountcenter.ErrInvalid):
		writeAuthError(w, http.StatusBadRequest, "能量筛选条件无效")
	case err != nil:
		writeAuthError(w, http.StatusInternalServerError, "能量服务暂不可用")
	default:
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
	}
}

func writeAccountEnergyCSV(w http.ResponseWriter, value accountcenter.EnergyLedger, includeActor bool) {
	headers := w.Header()
	headers.Set("Cache-Control", "no-store")
	headers.Set("Content-Type", "text/csv; charset=utf-8")
	headers.Set("Content-Disposition", `attachment; filename="energy-ledger-page.csv"`)
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Energy-Page-Limit", strconv.Itoa(value.Limit))
	headers.Set("X-Energy-Page-Offset", strconv.Itoa(value.Offset))
	headers.Set("X-Energy-Has-More", strconv.FormatBool(value.HasMore))
	if value.NextOffset != nil {
		headers.Set("X-Energy-Next-Offset", strconv.Itoa(*value.NextOffset))
	}
	writer := csv.NewWriter(w)
	columns := []string{"requestId", "owner", "at", "feature", "model", "status", "actualCost"}
	if includeActor {
		columns = append(columns, "actorUsername")
	}
	if err := writer.Write(columns); err != nil {
		return
	}
	for _, entry := range value.Entries {
		amount := ""
		if entry.ActualCost != nil {
			amount = *entry.ActualCost
		}
		cells := []string{energyCSVText(entry.RequestID), energyCSVText(entry.Owner), energyCSVText(entry.At.UTC().Format(time.RFC3339Nano)),
			energyCSVText(entry.Feature), energyCSVText(entry.Model), energyCSVText(entry.Status), amount}
		if includeActor {
			cells = append(cells, energyCSVText(entry.ActorUsername))
		}
		if err := writer.Write(cells); err != nil {
			return
		}
	}
	writer.Flush()
}

func energyCSVText(value string) string {
	candidate := strings.TrimLeftFunc(value, func(r rune) bool {
		// Keep whitespace controls in the candidate so they are still escaped.
		return r == '\uFEFF' || unicode.IsSpace(r) && !unicode.IsControl(r)
	})
	first, width := utf8.DecodeRuneInString(candidate)
	if width == 0 {
		return value
	}
	if unicode.IsControl(first) {
		return "'" + value
	}
	const formulaStarts = "=+-@−\t\r\n"
	// Also check the first source character alone. A following accent can
	// compose with a trigger, while the original still starts with that trigger.
	leading, _ := utf8.DecodeRuneInString(norm.NFKC.String(candidate[:width]))
	if strings.ContainsRune(formulaStarts, leading) {
		return "'" + value
	}
	// The export writes the original text. Inspect just its first normalized
	// segment so a long tail never requires a normalized copy of the field.
	var prefix norm.Iter
	prefix.InitString(norm.NFKC, candidate)
	leading, _ = utf8.DecodeRune(prefix.Next())
	if strings.ContainsRune(formulaStarts, leading) {
		return "'" + value
	}
	return value
}
