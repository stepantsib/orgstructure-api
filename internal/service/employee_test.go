package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"orgstructure/internal/domain"
	"orgstructure/internal/errs"
)

func TestCreateEmployee_HappyPath(t *testing.T) {
	svc, dRepo, eRepo := setupDept(t)
	_ = eRepo
	ctx := context.Background()

	d, _ := svc.Create(ctx, domain.CreateDepartmentInput{Name: "Engineering"})
	empSvc := NewEmployeeService(dRepo, eRepo)

	hired := &domain.Date{Time: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)}
	e, err := empSvc.Create(ctx, d.ID, domain.CreateEmployeeInput{
		FullName: "  Alice  ",
		Position: "  Senior Engineer  ",
		HiredAt:  hired,
	})
	require.NoError(t, err)
	assert.Equal(t, "Alice", e.FullName)
	assert.Equal(t, "Senior Engineer", e.Position)
	require.NotNil(t, e.HiredAt)
	assert.Equal(t, 2024, e.HiredAt.Year())
}

func TestCreateEmployee_MissingDepartment_404(t *testing.T) {
	_, dRepo, eRepo := setupDept(t)
	empSvc := NewEmployeeService(dRepo, eRepo)

	_, err := empSvc.Create(context.Background(), 999, domain.CreateEmployeeInput{
		FullName: "Alice", Position: "Engineer",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrNotFound)
}

func TestCreateEmployee_ValidationErrors(t *testing.T) {
	svc, dRepo, eRepo := setupDept(t)
	d, _ := svc.Create(context.Background(), domain.CreateDepartmentInput{Name: "Engineering"})
	empSvc := NewEmployeeService(dRepo, eRepo)

	_, err := empSvc.Create(context.Background(), d.ID, domain.CreateEmployeeInput{
		FullName: "   ",
		Position: "",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrValidation)
}
