package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"orgstructure/internal/domain"
	"orgstructure/internal/errs"
)

// EmployeeRepository owns persistence for domain.Employee.
type EmployeeRepository struct {
	db *gorm.DB
}

func NewEmployeeRepository(db *gorm.DB) *EmployeeRepository {
	return &EmployeeRepository{db: db}
}

// Create inserts a new employee. The service layer must have already verified
// that the parent department exists, so a FK violation here is a 500-class
// error (race between existence check and insert).
func (r *EmployeeRepository) Create(ctx context.Context, e *domain.Employee) error {
	if err := r.db.WithContext(ctx).Create(e).Error; err != nil {
		if isCheckViolation(err) {
			return fmt.Errorf("employee violates a check constraint: %w", errs.ErrBadRequest)
		}
		return err
	}
	return nil
}

// ListByDepartmentIDs fetches employees whose department is in `deptIDs`,
// sorted by the chosen field. Used by GET /departments/{id} to populate the
// tree response in a single query.
func (r *EmployeeRepository) ListByDepartmentIDs(
	ctx context.Context,
	deptIDs []uint64,
	sortBy domain.EmployeeSortField,
) ([]domain.Employee, error) {
	if len(deptIDs) == 0 {
		return nil, nil
	}

	orderClause := "created_at ASC, id ASC"
	if sortBy == domain.SortEmployeesByFullName {
		orderClause = "full_name ASC, id ASC"
	}

	var employees []domain.Employee
	err := r.db.WithContext(ctx).
		Where("department_id IN ?", deptIDs).
		Order(orderClause).
		Find(&employees).Error
	return employees, err
}

// CountByDepartment is a tiny helper used in tests / debug endpoints.
func (r *EmployeeRepository) CountByDepartment(ctx context.Context, deptID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&domain.Employee{}).
		Where("department_id = ?", deptID).
		Count(&n).Error
	return n, err
}
