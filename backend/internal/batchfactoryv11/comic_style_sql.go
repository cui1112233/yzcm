package batchfactoryv11

import (
	"context"
	"database/sql"
	"strings"
)

// scopeComicStyleSQL only receives static SELECT/UPDATE templates belonging to
// this package. The style is always a bound parameter, never SQL text.
func scopeComicStyleSQL(query string, args []any, styleID string) (string, []any) {
	cut := len(query)
	for _, suffix := range []string{" ORDER BY ", " LIMIT ", " FOR UPDATE"} {
		if n := strings.Index(query, suffix); n >= 0 && n < cut {
			cut = n
		}
	}
	column := "style_id"
	if strings.Contains(query, "batch_factory_v12_comic_pages p ") {
		column = "p.style_id"
	}
	position := strings.Count(query[:cut], "?")
	scoped := make([]any, 0, len(args)+1)
	scoped = append(scoped, args[:position]...)
	scoped = append(scoped, styleID)
	scoped = append(scoped, args[position:]...)
	return query[:cut] + " AND " + column + "=?" + query[cut:], scoped
}

type comicStyleSQLQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func queryComicStyleRow(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string, args ...any) *sql.Row {
	query, args = scopeComicStyleSQL(query, args, comicStyleID(ctx))
	return q.QueryRowContext(ctx, query, args...)
}
func queryComicStyleRows(ctx context.Context, q comicStyleSQLQueryer, query string, args ...any) (*sql.Rows, error) {
	query, args = scopeComicStyleSQL(query, args, comicStyleID(ctx))
	return q.QueryContext(ctx, query, args...)
}
func execComicStyle(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, query string, args ...any) (sql.Result, error) {
	query, args = scopeComicStyleSQL(query, args, comicStyleID(ctx))
	return q.ExecContext(ctx, query, args...)
}
