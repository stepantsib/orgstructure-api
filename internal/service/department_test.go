package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"orgstructure/internal/domain"
	"orgstructure/internal/errs"
)

func setupDept(t *testing.T) (*DepartmentService, *fakeDeptRepo, *fakeEmpRepo) {
	t.Helper()
	dRepo := newFakeDeptRepo()
	eRepo := newFakeEmpRepo()
	dRepo.empRef = eRepo
	return NewDepartmentService(dRepo, eRepo), dRepo, eRepo
}

func TestCreateDepartment_TrimsAndValidatesName(t *testing.T) {
	svc, _, _ := setupDept(t)
	ctx := context.Background()

	d, err := svc.Create(ctx, domain.CreateDepartmentInput{Name: "  Engineering  "})
	require.NoError(t, err)
	assert.Equal(t, "Engineering", d.Name)
	assert.Nil(t, d.ParentID)

	_, err = svc.Create(ctx, domain.CreateDepartmentInput{Name: "   "})
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrValidation)
}

func TestCreateDepartment_DuplicateNameInSameParent_409(t *testing.T) {
	svc, _, _ := setupDept(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, domain.CreateDepartmentInput{Name: "Backend"})
	require.NoError(t, err)

	_, err = svc.Create(ctx, domain.CreateDepartmentInput{Name: "Backend"})
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrConflict)
}

func TestCreateDepartment_SameNameInDifferentParents_OK(t *testing.T) {
	svc, _, _ := setupDept(t)
	ctx := context.Background()

	a, err := svc.Create(ctx, domain.CreateDepartmentInput{Name: "A"})
	require.NoError(t, err)
	b, err := svc.Create(ctx, domain.CreateDepartmentInput{Name: "B"})
	require.NoError(t, err)

	_, err = svc.Create(ctx, domain.CreateDepartmentInput{Name: "Squad", ParentID: &a.ID})
	require.NoError(t, err)
	_, err = svc.Create(ctx, domain.CreateDepartmentInput{Name: "Squad", ParentID: &b.ID})
	require.NoError(t, err, "same name allowed under different parents")
}

func TestCreateDepartment_UnknownParent_404(t *testing.T) {
	svc, _, _ := setupDept(t)
	ctx := context.Background()

	missing := uint64(999)
	_, err := svc.Create(ctx, domain.CreateDepartmentInput{Name: "Orphan", ParentID: &missing})
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrNotFound)
}

func TestUpdateDepartment_CycleDetection(t *testing.T) {
	svc, _, _ := setupDept(t)
	ctx := context.Background()

	a, err := svc.Create(ctx, domain.CreateDepartmentInput{Name: "A"})
	require.NoError(t, err)
	b, err := svc.Create(ctx, domain.CreateDepartmentInput{Name: "B", ParentID: &a.ID})
	require.NoError(t, err)
	c, err := svc.Create(ctx, domain.CreateDepartmentInput{Name: "C", ParentID: &b.ID})
	require.NoError(t, err)

	// Moving A under its descendant C must fail with 409.
	in := domain.UpdateDepartmentInput{
		ParentID: domain.Nullable[uint64]{Set: true, Valid: true, Value: c.ID},
	}
	_, err = svc.Update(ctx, a.ID, in)
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrConflict)

	// Self-parent must also fail.
	in = domain.UpdateDepartmentInput{
		ParentID: domain.Nullable[uint64]{Set: true, Valid: true, Value: a.ID},
	}
	_, err = svc.Update(ctx, a.ID, in)
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrConflict)
}

func TestUpdateDepartment_PromoteToRootWithExplicitNull(t *testing.T) {
	svc, _, _ := setupDept(t)
	ctx := context.Background()

	a, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "A"})
	b, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "B", ParentID: &a.ID})

	in := domain.UpdateDepartmentInput{
		ParentID: domain.Nullable[uint64]{Set: true, Valid: false},
	}
	d, err := svc.Update(ctx, b.ID, in)
	require.NoError(t, err)
	assert.Nil(t, d.ParentID)
}

func TestUpdateDepartment_RenameDoesNotCollideWithAnotherParent(t *testing.T) {
	svc, _, _ := setupDept(t)
	ctx := context.Background()

	root, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "Root"})
	_, _ = svc.Create(ctx, domain.CreateDepartmentInput{Name: "Backend", ParentID: &root.ID})
	c, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "Frontend", ParentID: &root.ID})

	in := domain.UpdateDepartmentInput{
		Name: domain.Nullable[string]{Set: true, Valid: true, Value: "Backend"},
	}
	_, err := svc.Update(ctx, c.ID, in)
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrConflict)
}

func TestDeleteDepartment_CascadeRemovesSubtree(t *testing.T) {
	svc, dRepo, _ := setupDept(t)
	ctx := context.Background()

	a, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "A"})
	b, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "B", ParentID: &a.ID})
	c, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "C", ParentID: &b.ID})

	require.NoError(t, svc.Delete(ctx, a.ID, domain.DeleteModeCascade, nil))

	for _, id := range []uint64{a.ID, b.ID, c.ID} {
		_, err := dRepo.GetByID(ctx, id)
		assert.True(t, errors.Is(err, errs.ErrNotFound), "id %d should be gone", id)
	}
}

func TestDeleteDepartment_ReassignMovesEmployeesAndDeletesSubtree(t *testing.T) {
	svc, dRepo, eRepo := setupDept(t)
	ctx := context.Background()

	src, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "Src"})
	tgt, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "Tgt"})

	for _, n := range []string{"Alice", "Bob"} {
		require.NoError(t, eRepo.Create(ctx, &domain.Employee{
			DepartmentID: src.ID,
			FullName:     n,
			Position:     "Engineer",
		}))
	}

	require.NoError(t, svc.Delete(ctx, src.ID, domain.DeleteModeReassign, &tgt.ID))

	emps, err := eRepo.ListByDepartmentIDs(ctx, []uint64{tgt.ID}, domain.SortEmployeesByFullName)
	require.NoError(t, err)
	assert.Len(t, emps, 2, "both employees moved to target")

	_, err = dRepo.GetByID(ctx, src.ID)
	assert.ErrorIs(t, err, errs.ErrNotFound)
}

func TestDeleteDepartment_ReassignTargetInsideSubtree_409(t *testing.T) {
	svc, _, _ := setupDept(t)
	ctx := context.Background()

	a, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "A"})
	b, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "B", ParentID: &a.ID})

	err := svc.Delete(ctx, a.ID, domain.DeleteModeReassign, &b.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrConflict)
}

func TestDeleteDepartment_ReassignMissingTarget(t *testing.T) {
	svc, _, _ := setupDept(t)
	ctx := context.Background()

	a, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "A"})
	err := svc.Delete(ctx, a.ID, domain.DeleteModeReassign, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrBadRequest)
}

func TestGetTree_RespectsDepth(t *testing.T) {
	svc, _, _ := setupDept(t)
	ctx := context.Background()

	a, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "A"})
	b, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "B", ParentID: &a.ID})
	_, _ = svc.Create(ctx, domain.CreateDepartmentInput{Name: "C", ParentID: &b.ID})

	tree, err := svc.GetTree(ctx, a.ID, 1, false, domain.SortEmployeesByCreatedAt)
	require.NoError(t, err)
	require.Len(t, tree.Children, 1)
	assert.Empty(t, tree.Children[0].Children, "depth=1 stops after direct children")

	tree, err = svc.GetTree(ctx, a.ID, 2, false, domain.SortEmployeesByCreatedAt)
	require.NoError(t, err)
	require.Len(t, tree.Children, 1)
	require.Len(t, tree.Children[0].Children, 1, "depth=2 includes grandchildren")
}

func TestGetTree_IncludesEmployees(t *testing.T) {
	svc, _, eRepo := setupDept(t)
	ctx := context.Background()

	a, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "A"})
	require.NoError(t, eRepo.Create(ctx, &domain.Employee{
		DepartmentID: a.ID, FullName: "Alice", Position: "Lead",
	}))

	tree, err := svc.GetTree(ctx, a.ID, 0, true, domain.SortEmployeesByCreatedAt)
	require.NoError(t, err)
	require.NotNil(t, tree.Employees)
	assert.Len(t, *tree.Employees, 1)
	assert.Equal(t, "Alice", (*tree.Employees)[0].FullName)
}

func TestGetTree_MissingDepartment_404(t *testing.T) {
	svc, _, _ := setupDept(t)
	_, err := svc.GetTree(context.Background(), 999, 1, false, domain.SortEmployeesByCreatedAt)
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrNotFound)
}
