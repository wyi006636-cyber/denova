package projection

import (
	"context"
	"database/sql"
	"fmt"
)

type projectionSQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type projectionSQLConnection interface {
	projectionSQLExecutor
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func runExternalContentIntegrityCheck(ctx context.Context, executor projectionSQLExecutor, purpose IntegrityPurpose, hooks Hooks) error {
	if _, err := executor.ExecContext(ctx, externalContentIntegritySQL); err != nil {
		return fmt.Errorf("Projection external-content integrity purpose=%s: %w", purpose, err)
	}
	hooks.integrity(purpose)
	return nil
}
