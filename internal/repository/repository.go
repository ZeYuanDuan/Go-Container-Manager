package repository

import "context"

// TxManager runs a function within a database transaction.
// Used for operations that need row-level locking and atomic multi-repo updates.
// PostgreSQL implementation: internal/repository/postgres.
type TxManager interface {
	WithTx(ctx context.Context, fn func(context.Context) error) error
}
