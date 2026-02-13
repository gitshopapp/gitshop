package db

import (
	"context"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5"
)

type querySpanContextKey struct{}

type queryTracer struct{}

func newQueryTracer() *queryTracer {
	return &queryTracer{}
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if sentry.SpanFromContext(ctx) == nil {
		return ctx
	}

	query := normalizeQuery(data.SQL)
	span := sentry.StartSpan(
		ctx,
		"db.query",
		sentry.WithDescription(query),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	span.SetData("db.system", "postgresql")

	if operation := queryOperation(query); operation != "" {
		span.SetData("db.operation", operation)
	}

	ctx = context.WithValue(span.Context(), querySpanContextKey{}, span)
	return ctx
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, _ := ctx.Value(querySpanContextKey{}).(*sentry.Span)
	if span == nil {
		return
	}

	if data.Err != nil {
		span.Status = sentry.SpanStatusInternalError
		span.SetData("db.error", data.Err.Error())
	} else {
		span.Status = sentry.SpanStatusOK
	}

	rowsAffected := data.CommandTag.RowsAffected()
	if rowsAffected >= 0 {
		span.SetData("db.rows_affected", rowsAffected)
	}

	span.Finish()
}

func normalizeQuery(query string) string {
	normalized := strings.TrimSpace(query)
	if normalized == "" {
		return "sql.query"
	}

	normalized = strings.Join(strings.Fields(normalized), " ")
	const maxLen = 512
	if len(normalized) > maxLen {
		return normalized[:maxLen]
	}
	return normalized
}

func queryOperation(query string) string {
	if query == "" {
		return ""
	}

	parts := strings.Fields(query)
	if len(parts) == 0 {
		return ""
	}
	return strings.ToUpper(parts[0])
}
