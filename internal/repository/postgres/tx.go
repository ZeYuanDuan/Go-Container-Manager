package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/alsonduan/go-container-manager/internal/repository"
)

type txKey struct{}

var ctxKeyTx = txKey{}

type postgresTxManager struct {
	pool *pgxpool.Pool
}

// NewTxManager creates a TxManager backed by the given pool.
func NewTxManager(pool *pgxpool.Pool) repository.TxManager {
	return &postgresTxManager{pool: pool}
}

func (m *postgresTxManager) WithTx(ctx context.Context, fn func(context.Context) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	txCtx := context.WithValue(ctx, ctxKeyTx, tx)
	err = fn(txCtx)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// txFromContext returns the pgx.Tx from context if present, else nil.
func txFromContext(ctx context.Context) pgx.Tx {
	if tx, ok := ctx.Value(ctxKeyTx).(pgx.Tx); ok {
		return tx
	}
	return nil
}

// queryable is either *pgxpool.Pool or pgx.Tx for executing queries.
type queryable interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// getQueryable returns the tx from context if in a transaction, else the pool.
func getQueryable(ctx context.Context, pool *pgxpool.Pool) queryable {
	if tx := txFromContext(ctx); tx != nil {
		return tx
	}
	return pool
}
