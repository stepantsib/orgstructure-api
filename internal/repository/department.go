package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"orgstructure/internal/domain"
	"orgstructure/internal/errs"
)

// DepartmentRepository owns persistence for domain.Department.
type DepartmentRepository struct {
	db *gorm.DB
}

func NewDepartmentRepository(db *gorm.DB) *DepartmentRepository {
	return &DepartmentRepository{db: db}
}

// DB exposes the underlying connection for cases where the service layer
// needs to run a transaction spanning multiple repositories.
func (r *DepartmentRepository) DB() *gorm.DB { return r.db }

// Create inserts a new department. Unique-name conflicts within the same parent
// are translated to errs.ErrConflict so callers can return 409 directly.
func (r *DepartmentRepository) Create(ctx context.Context, d *domain.Department) error {
	if err := r.db.WithContext(ctx).Create(d).Error; err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("department name already exists in this parent: %w", errs.ErrConflict)
		}
		if isCheckViolation(err) {
			return fmt.Errorf("department violates a check constraint: %w", errs.ErrBadRequest)
		}
		return err
	}
	return nil
}

// GetByID returns a single department or errs.ErrNotFound.
func (r *DepartmentRepository) GetByID(ctx context.Context, id uint64) (*domain.Department, error) {
	var d domain.Department
	if err := r.db.WithContext(ctx).First(&d, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

// Exists is a lightweight check used by the service before inserting employees
// or moving departments.
func (r *DepartmentRepository) Exists(ctx context.Context, id uint64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&domain.Department{}).
		Where("id = ?", id).
		Count(&count).Error
	return count > 0, err
}

// Update applies a partial update and returns the refreshed entity.
// `updates` is a map so callers can opt in / out of each column, and nil
// values are written as SQL NULL (GORM-map semantics).
func (r *DepartmentRepository) Update(ctx context.Context, id uint64, updates map[string]any) (*domain.Department, error) {
	if len(updates) > 0 {
		err := r.db.WithContext(ctx).
			Model(&domain.Department{}).
			Where("id = ?", id).
			Updates(updates).Error
		if err != nil {
			if isUniqueViolation(err) {
				return nil, fmt.Errorf("department name already exists in this parent: %w", errs.ErrConflict)
			}
			if isCheckViolation(err) {
				return nil, fmt.Errorf("department violates a check constraint: %w", errs.ErrBadRequest)
			}
			return nil, err
		}
	}
	return r.GetByID(ctx, id)
}

// SubtreeIDs returns every department id at or below `rootID`, in BFS order.
// Used both for cycle detection (move) and cascade enforcement.
func (r *DepartmentRepository) SubtreeIDs(ctx context.Context, rootID uint64) ([]uint64, error) {
	const q = `
		WITH RECURSIVE subtree AS (
			SELECT id FROM departments WHERE id = ?
			UNION ALL
			SELECT d.id
			FROM departments d
			INNER JOIN subtree s ON d.parent_id = s.id
		)
		SELECT id FROM subtree;
	`
	var ids []uint64
	if err := r.db.WithContext(ctx).Raw(q, rootID).Scan(&ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// SubtreeDepartments returns all departments at or below `rootID`, limited by
// the requested depth (0 = only root, 1 = root + direct children, etc.).
// Results are ordered by depth, then id, so the service can assemble the tree
// in a single pass.
func (r *DepartmentRepository) SubtreeDepartments(ctx context.Context, rootID uint64, depth int) ([]domain.Department, error) {
	const q = `
		WITH RECURSIVE subtree AS (
			SELECT id, name, parent_id, created_at, 0 AS depth
			FROM departments WHERE id = ?
			UNION ALL
			SELECT d.id, d.name, d.parent_id, d.created_at, s.depth + 1
			FROM departments d
			INNER JOIN subtree s ON d.parent_id = s.id
			WHERE s.depth < ?
		)
		SELECT id, name, parent_id, created_at
		FROM subtree
		ORDER BY depth, id;
	`
	var rows []domain.Department
	if err := r.db.WithContext(ctx).Raw(q, rootID, depth).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Delete removes a department by id. The schema's ON DELETE CASCADE ensures
// child departments and employees disappear as well.
func (r *DepartmentRepository) Delete(ctx context.Context, id uint64) error {
	tx := r.db.WithContext(ctx).Delete(&domain.Department{}, id)
	if err := tx.Error; err != nil {
		return err
	}
	if tx.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}

// ReassignAndDelete moves every employee under `id`'s subtree to `targetID`
// and then deletes the subtree, atomically.
func (r *DepartmentRepository) ReassignAndDelete(ctx context.Context, id, targetID uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Collect subtree ids — the source department and all descendants.
		var ids []uint64
		const subtreeQ = `
			WITH RECURSIVE subtree AS (
				SELECT id FROM departments WHERE id = ?
				UNION ALL
				SELECT d.id FROM departments d INNER JOIN subtree s ON d.parent_id = s.id
			)
			SELECT id FROM subtree;
		`
		if err := tx.Raw(subtreeQ, id).Scan(&ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return errs.ErrNotFound
		}

		// 2. Reassign employees to the target. The target cannot be inside the
		//    subtree (the service layer rejects that before we ever get here).
		if err := tx.
			Model(&domain.Employee{}).
			Where("department_id IN ?", ids).
			Update("department_id", targetID).Error; err != nil {
			return err
		}

		// 3. Delete the root; ON DELETE CASCADE wipes the children.
		if err := tx.Delete(&domain.Department{}, id).Error; err != nil {
			return err
		}
		return nil
	})
}

// NameExistsInParent reports whether a sibling with `name` already lives under
// `parentID`. Used to surface a 409 before the DB raises a unique violation
// (and to clearly distinguish "name conflict" from other unique constraints).
func (r *DepartmentRepository) NameExistsInParent(ctx context.Context, parentID *uint64, name string, excludeID uint64) (bool, error) {
	q := r.db.WithContext(ctx).Model(&domain.Department{}).Where("name = ?", name)
	if parentID == nil {
		q = q.Where("parent_id IS NULL")
	} else {
		q = q.Where("parent_id = ?", *parentID)
	}
	if excludeID != 0 {
		q = q.Where("id <> ?", excludeID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
