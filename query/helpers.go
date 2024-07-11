package query

import (
	"context"
	"github.com/danthegoodman1/GoAPITemplate/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	oteltrace "go.opentelemetry.io/otel/trace"
	"strings"
	"time"
)

var tracer = otel.GetTracerProvider().Tracer("sql")

func createSpan(ctx context.Context, s string) (context.Context, oteltrace.Span) {
	queryName, _, _ := strings.Cut(s, "\n")
	opts := []oteltrace.SpanStartOption{
		oteltrace.WithSpanKind(oteltrace.SpanKindServer),
	}
	ctx, span := tracer.Start(ctx, queryName, opts...)
	return ctx, span
}

// Note: *Queries will be generated when sqlc generates stuff

func ReliableExec(ctx context.Context, pool *pgxpool.Pool, tryTimeout time.Duration, f func(ctx context.Context, q *Queries) error) error {
	ctx, span := createSpan(ctx, "ReliableExec")
	defer span.End()
	return utils.ReliableExec(ctx, pool, tryTimeout, func(ctx context.Context, conn *pgxpool.Conn) error {
		return f(ctx, NewWithTracing(conn))
	})
}

func ReliableExecInTx(ctx context.Context, pool *pgxpool.Pool, tryTimeout time.Duration, f func(ctx context.Context, q *Queries) error) error {
	ctx, span := createSpan(ctx, "ReliableExecInTx")
	defer span.End()
	return utils.ReliableExecInTx(ctx, pool, tryTimeout, func(ctx context.Context, conn pgx.Tx) error {
		return f(ctx, NewWithTracing(conn))
	})
}
