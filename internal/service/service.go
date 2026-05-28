// Package service holds the business logic. It is the only layer that knows
// the rules: when a name conflict means 409, when an empty input means 422,
// when a cycle is being created, etc. Repositories handle persistence;
// handlers handle transport; both call into here.
package service

import (
	"context"

	"orgstructure/internal/domain"
)

// DepartmentRepo is the contract the DepartmentService expects.
// Defining it here (next to the consumer) keeps the layers decoupled.
type DepartmentRepo interface {
	Create(ctx context.Context, d *domain.Department) error
	GetByID(ctx context.Context, id uint64) (*domain.Department, error)
	Exists(ctx context.Context, id uint64) (bool, error)
	Update(ctx context.Context, id uint64, updates map[string]any) (*domain.Department, error)
	SubtreeIDs(ctx context.Context, rootID uint64) ([]uint64, error)
	SubtreeDepartments(ctx context.Context, rootID uint64, depth int) ([]domain.Department, error)
	Delete(ctx context.Context, id uint64) error
	ReassignAndDelete(ctx context.Context, id, targetID uint64) error
	NameExistsInParent(ctx context.Context, parentID *uint64, name string, excludeID uint64) (bool, error)
}

type EmployeeRepo interface {
	Create(ctx context.Context, e *domain.Employee) error
	ListByDepartmentIDs(ctx context.Context, ids []uint64, sortBy domain.EmployeeSortField) ([]domain.Employee, error)
}

// Services bundles the public services for wiring in main.
type Services struct {
	Departments *DepartmentService
	Employees   *EmployeeService
}

func New(deptRepo DepartmentRepo, empRepo EmployeeRepo) *Services {
	return &Services{
		Departments: NewDepartmentService(deptRepo, empRepo),
		Employees:   NewEmployeeService(deptRepo, empRepo),
	}
}
