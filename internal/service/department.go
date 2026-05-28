package service

import (
	"context"
	"fmt"

	"orgstructure/internal/domain"
	"orgstructure/internal/errs"
	"orgstructure/internal/validator"
)

// DepartmentService implements the rules around departments:
// uniqueness within a parent, cycle prevention, and the cascade/reassign
// strategies on delete.
type DepartmentService struct {
	repo    DepartmentRepo
	empRepo EmployeeRepo
}

func NewDepartmentService(repo DepartmentRepo, empRepo EmployeeRepo) *DepartmentService {
	return &DepartmentService{repo: repo, empRepo: empRepo}
}

// MaxTreeDepth is enforced for GET responses so a malicious caller cannot
// request an arbitrarily deep traversal.
const MaxTreeDepth = 5

// Create validates the input and inserts a new department.
func (s *DepartmentService) Create(ctx context.Context, in domain.CreateDepartmentInput) (*domain.Department, error) {
	ve := errs.NewValidation()
	name := validator.NonEmptyString("name", in.Name, ve)
	if ve.HasErrors() {
		return nil, ve
	}

	if in.ParentID != nil {
		exists, err := s.repo.Exists(ctx, *in.ParentID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("parent department %d: %w", *in.ParentID, errs.ErrNotFound)
		}
	}

	// Pre-check uniqueness so we return a clean 409 instead of leaking the
	// DB error string. The unique index in the schema is still the source of
	// truth and protects against the racing-insert case.
	conflict, err := s.repo.NameExistsInParent(ctx, in.ParentID, name, 0)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, fmt.Errorf("department name %q already exists in this parent: %w", name, errs.ErrConflict)
	}

	d := &domain.Department{
		Name:     name,
		ParentID: in.ParentID,
	}
	if err := s.repo.Create(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// GetTree assembles the recursive response for GET /departments/{id} up to
// `depth` levels. When `includeEmployees` is true, every department in the
// returned subtree is populated with its employees sorted by `sortBy`.
func (s *DepartmentService) GetTree(
	ctx context.Context,
	id uint64,
	depth int,
	includeEmployees bool,
	sortBy domain.EmployeeSortField,
) (*domain.DepartmentTreeNode, error) {
	if depth < 0 {
		depth = 0
	}
	if depth > MaxTreeDepth {
		depth = MaxTreeDepth
	}

	rows, err := s.repo.SubtreeDepartments(ctx, id, depth)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("department %d: %w", id, errs.ErrNotFound)
	}

	// Build a map id -> node and link children to their parents.
	// SubtreeDepartments returns rows ordered by depth, so a parent always
	// appears before its children — one pass is enough.
	nodes := make(map[uint64]*domain.DepartmentTreeNode, len(rows))
	var root *domain.DepartmentTreeNode

	for i := range rows {
		node := &domain.DepartmentTreeNode{
			Department: rows[i],
			Children:   []*domain.DepartmentTreeNode{},
		}
		nodes[rows[i].ID] = node

		if rows[i].ID == id {
			root = node
			continue
		}
		// Every non-root row has its parent already in the map (depth-ordered).
		if parent, ok := nodes[*rows[i].ParentID]; ok {
			parent.Children = append(parent.Children, node)
		}
	}

	if includeEmployees {
		// Seed every node with an empty slice so that, after the response is
		// rendered, each department carries an `employees: []` field even if
		// nobody works there.
		for _, n := range nodes {
			empty := []domain.Employee{}
			n.Employees = &empty
		}

		ids := make([]uint64, 0, len(nodes))
		for id := range nodes {
			ids = append(ids, id)
		}
		emps, err := s.empRepo.ListByDepartmentIDs(ctx, ids, sortBy)
		if err != nil {
			return nil, err
		}
		for _, e := range emps {
			if n, ok := nodes[e.DepartmentID]; ok {
				*n.Employees = append(*n.Employees, e)
			}
		}
	}

	return root, nil
}

// Update applies a partial update to a department, including a possible
// parent change. Cycle prevention runs before persisting.
func (s *DepartmentService) Update(
	ctx context.Context,
	id uint64,
	in domain.UpdateDepartmentInput,
) (*domain.Department, error) {
	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	ve := errs.NewValidation()
	updates := map[string]any{}

	if in.Name.Set {
		if !in.Name.Valid {
			ve.Add("name", "must not be null")
		} else {
			name := validator.NonEmptyString("name", in.Name.Value, ve)
			if !ve.HasErrors() {
				updates["name"] = name
			}
		}
	}

	var newParent *uint64
	parentChanged := false
	if in.ParentID.Set {
		parentChanged = true
		if in.ParentID.Valid {
			pid := in.ParentID.Value
			if pid == id {
				return nil, fmt.Errorf("a department cannot be its own parent: %w", errs.ErrConflict)
			}
			exists, err := s.repo.Exists(ctx, pid)
			if err != nil {
				return nil, err
			}
			if !exists {
				return nil, fmt.Errorf("parent department %d: %w", pid, errs.ErrNotFound)
			}
			// Cycle check: the new parent must not live inside this department's subtree.
			subtree, err := s.repo.SubtreeIDs(ctx, id)
			if err != nil {
				return nil, err
			}
			for _, sid := range subtree {
				if sid == pid {
					return nil, fmt.Errorf("cannot move department %d under its own descendant %d: %w", id, pid, errs.ErrConflict)
				}
			}
			newParent = &pid
			updates["parent_id"] = pid
		} else {
			// Explicit null → promote to root.
			updates["parent_id"] = nil
		}
	}

	if ve.HasErrors() {
		return nil, ve
	}

	// Decide effective name + parent for the duplicate check.
	effectiveName := current.Name
	if v, ok := updates["name"].(string); ok {
		effectiveName = v
	}
	effectiveParent := current.ParentID
	if parentChanged {
		effectiveParent = newParent // may be nil
	}

	if _, nameTouched := updates["name"]; nameTouched || parentChanged {
		conflict, err := s.repo.NameExistsInParent(ctx, effectiveParent, effectiveName, id)
		if err != nil {
			return nil, err
		}
		if conflict {
			return nil, fmt.Errorf("department name %q already exists in this parent: %w", effectiveName, errs.ErrConflict)
		}
	}

	if len(updates) == 0 {
		// Nothing to change — return the current entity untouched.
		return current, nil
	}

	updated, err := s.repo.Update(ctx, id, updates)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// Delete removes a department using the requested strategy.
//
//   - cascade  → DB-level ON DELETE CASCADE drops the whole subtree.
//   - reassign → every employee in the subtree is moved to `reassignTo`
//     first, then the subtree is dropped. `reassignTo` must exist and
//     must NOT be inside the subtree being deleted.
func (s *DepartmentService) Delete(
	ctx context.Context,
	id uint64,
	mode domain.DeleteMode,
	reassignTo *uint64,
) error {
	if !mode.Valid() {
		return fmt.Errorf("mode must be 'cascade' or 'reassign': %w", errs.ErrBadRequest)
	}

	exists, err := s.repo.Exists(ctx, id)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("department %d: %w", id, errs.ErrNotFound)
	}

	switch mode {
	case domain.DeleteModeCascade:
		return s.repo.Delete(ctx, id)

	case domain.DeleteModeReassign:
		if reassignTo == nil {
			return fmt.Errorf("reassign_to_department_id is required when mode=reassign: %w", errs.ErrBadRequest)
		}
		if *reassignTo == id {
			return fmt.Errorf("reassign target must differ from the department being deleted: %w", errs.ErrBadRequest)
		}
		targetExists, err := s.repo.Exists(ctx, *reassignTo)
		if err != nil {
			return err
		}
		if !targetExists {
			return fmt.Errorf("reassign target department %d: %w", *reassignTo, errs.ErrNotFound)
		}
		// The target must live outside the subtree we are about to delete,
		// otherwise employees would be moved into a department that is about
		// to be cascade-deleted.
		subtree, err := s.repo.SubtreeIDs(ctx, id)
		if err != nil {
			return err
		}
		for _, sid := range subtree {
			if sid == *reassignTo {
				return fmt.Errorf("reassign target %d must not be inside the deleted subtree: %w", *reassignTo, errs.ErrConflict)
			}
		}
		return s.repo.ReassignAndDelete(ctx, id, *reassignTo)
	}

	return nil
}
