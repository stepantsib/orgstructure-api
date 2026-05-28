package service

import (
	"context"
	"fmt"

	"orgstructure/internal/domain"
	"orgstructure/internal/errs"
	"orgstructure/internal/validator"
)

// EmployeeService handles employee-level rules: payload validation and
// parent-department existence checks (the spec mandates 404 when the
// department is missing).
type EmployeeService struct {
	deptRepo DepartmentRepo
	empRepo  EmployeeRepo
}

func NewEmployeeService(deptRepo DepartmentRepo, empRepo EmployeeRepo) *EmployeeService {
	return &EmployeeService{deptRepo: deptRepo, empRepo: empRepo}
}

// normalizeDate folds an explicit "zero" date into nil so the column ends up
// as SQL NULL rather than a 0001-01-01 sentinel.
func normalizeDate(d *domain.Date) *domain.Date {
	if d == nil || d.Time.IsZero() {
		return nil
	}
	return d
}

// Create validates input and inserts the employee. Returns ErrNotFound when
// the department does not exist (spec: "Нельзя создать сотрудника в
// несуществующем подразделении").
func (s *EmployeeService) Create(
	ctx context.Context,
	deptID uint64,
	in domain.CreateEmployeeInput,
) (*domain.Employee, error) {
	exists, err := s.deptRepo.Exists(ctx, deptID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("department %d: %w", deptID, errs.ErrNotFound)
	}

	ve := errs.NewValidation()
	fullName := validator.NonEmptyString("full_name", in.FullName, ve)
	position := validator.NonEmptyString("position", in.Position, ve)
	if ve.HasErrors() {
		return nil, ve
	}

	e := &domain.Employee{
		DepartmentID: deptID,
		FullName:     fullName,
		Position:     position,
		HiredAt:      normalizeDate(in.HiredAt),
	}
	if err := s.empRepo.Create(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}
