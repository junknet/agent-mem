package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

type pgxTx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}
