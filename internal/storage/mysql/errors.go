package mysql

import (
	"database/sql"
	"errors"
	"fmt"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/steveyegge/beads/internal/storage"
)

// Sentinel errors for the mysql storage layer. They mirror the dolt backend's
// sentinels so callers that already match on those can match the same way here.
var (
	// ErrTransaction indicates a transaction begin/commit/rollback failure.
	ErrTransaction = errors.New("transaction error")

	// ErrQuery indicates a database query failure.
	ErrQuery = errors.New("query error")

	// ErrScan indicates a failure scanning database rows into Go values.
	ErrScan = errors.New("scan error")

	// ErrExec indicates a database exec (INSERT/UPDATE/DELETE) failure.
	ErrExec = errors.New("exec error")
)

// isTableNotExistError returns true when err is MySQL error 1146.
func isTableNotExistError(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1146
}

// isSerializationError returns true if err is a MySQL serialization
// failure that guarantees the transaction was rolled back.
//
//   - 1213 (ER_LOCK_DEADLOCK)
//   - 1205 (ER_LOCK_WAIT_TIMEOUT)
func isSerializationError(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	return mysqlErr.Number == 1213 || mysqlErr.Number == 1205
}

// wrapDBError wraps a database error with operation context. sql.ErrNoRows is
// converted to storage.ErrNotFound. nil passes through.
func wrapDBError(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, storage.ErrNotFound)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func wrapTransactionError(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w: %w", op, ErrTransaction, err)
}

func wrapScanError(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w: %w", op, ErrScan, err)
}

func wrapQueryError(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w: %w", op, ErrQuery, err)
}

func wrapExecError(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w: %w", op, ErrExec, err)
}
