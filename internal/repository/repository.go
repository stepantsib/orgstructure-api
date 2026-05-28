// Package repository wraps GORM access to the database, hiding driver-specific
// errors behind the typed errors in internal/errs.
package repository

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// PG SQLSTATE codes we care about; see https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	pgCheckViolation      = "23514"
)

// isUniqueViolation returns true when err corresponds to a Postgres unique
// constraint conflict. Useful for translating duplicate-name errors into 409.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgUniqueViolation
	}
	return false
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgCheckViolation
	}
	return false
}

// New initializes all repositories sharing a single *gorm.DB.
type Repositories struct {
	Departments *DepartmentRepository
	Employees   *EmployeeRepository
}

func New(db *gorm.DB) *Repositories {
	return &Repositories{
		Departments: NewDepartmentRepository(db),
		Employees:   NewEmployeeRepository(db),
	}
}
