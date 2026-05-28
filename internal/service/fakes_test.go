package service

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"orgstructure/internal/domain"
	"orgstructure/internal/errs"
)

// fakeDeptRepo is an in-memory implementation of DepartmentRepo used for unit
// tests. It mirrors the real Postgres behavior closely enough that the service
// logic can be exercised without a database: descendants are walked for
// SubtreeIDs/SubtreeDepartments and uniqueness is enforced per parent.
type fakeDeptRepo struct {
	mu     sync.Mutex
	nextID uint64
	items  map[uint64]*domain.Department
	// empRef is the companion employee repo; set via setupDept so that
	// ReassignAndDelete can mutate employees alongside the subtree removal.
	empRef *fakeEmpRepo
}

func newFakeDeptRepo() *fakeDeptRepo {
	return &fakeDeptRepo{items: map[uint64]*domain.Department{}}
}

func (r *fakeDeptRepo) Create(_ context.Context, d *domain.Department) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.items {
		if existing.Name == d.Name && samePtr(existing.ParentID, d.ParentID) {
			return errs.ErrConflict
		}
	}
	r.nextID++
	d.ID = r.nextID
	d.CreatedAt = time.Now()
	clone := *d
	r.items[d.ID] = &clone
	return nil
}

func (r *fakeDeptRepo) GetByID(_ context.Context, id uint64) (*domain.Department, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.items[id]
	if !ok {
		return nil, errs.ErrNotFound
	}
	c := *d
	return &c, nil
}

func (r *fakeDeptRepo) Exists(_ context.Context, id uint64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.items[id]
	return ok, nil
}

func (r *fakeDeptRepo) Update(_ context.Context, id uint64, updates map[string]any) (*domain.Department, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.items[id]
	if !ok {
		return nil, errs.ErrNotFound
	}
	if v, ok := updates["name"].(string); ok {
		d.Name = v
	}
	if v, present := updates["parent_id"]; present {
		switch t := v.(type) {
		case uint64:
			d.ParentID = &t
		case nil:
			d.ParentID = nil
		}
	}
	c := *d
	return &c, nil
}

func (r *fakeDeptRepo) SubtreeIDs(_ context.Context, rootID uint64) ([]uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[rootID]; !ok {
		return nil, nil
	}
	out := []uint64{rootID}
	frontier := []uint64{rootID}
	for len(frontier) > 0 {
		next := []uint64{}
		for _, d := range r.items {
			if d.ParentID == nil {
				continue
			}
			for _, f := range frontier {
				if *d.ParentID == f {
					out = append(out, d.ID)
					next = append(next, d.ID)
				}
			}
		}
		frontier = next
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (r *fakeDeptRepo) SubtreeDepartments(_ context.Context, rootID uint64, depth int) ([]domain.Department, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	root, ok := r.items[rootID]
	if !ok {
		return nil, nil
	}
	type levelled struct {
		d     domain.Department
		level int
	}
	out := []levelled{{d: *root, level: 0}}
	frontier := []uint64{rootID}
	for lvl := 1; lvl <= depth && len(frontier) > 0; lvl++ {
		next := []uint64{}
		ids := make([]uint64, 0, len(r.items))
		for id := range r.items {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			d := r.items[id]
			if d.ParentID == nil {
				continue
			}
			for _, f := range frontier {
				if *d.ParentID == f {
					out = append(out, levelled{d: *d, level: lvl})
					next = append(next, d.ID)
				}
			}
		}
		frontier = next
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].level != out[j].level {
			return out[i].level < out[j].level
		}
		return out[i].d.ID < out[j].d.ID
	})
	res := make([]domain.Department, len(out))
	for i, lv := range out {
		res[i] = lv.d
	}
	return res, nil
}

func (r *fakeDeptRepo) Delete(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return errs.ErrNotFound
	}
	toRemove := map[uint64]bool{id: true}
	for {
		added := false
		for _, d := range r.items {
			if d.ParentID != nil && toRemove[*d.ParentID] && !toRemove[d.ID] {
				toRemove[d.ID] = true
				added = true
			}
		}
		if !added {
			break
		}
	}
	for id := range toRemove {
		delete(r.items, id)
	}
	if r.empRef != nil {
		r.empRef.deleteByDept(toRemove)
	}
	return nil
}

func (r *fakeDeptRepo) ReassignAndDelete(_ context.Context, id, target uint64) error {
	r.mu.Lock()
	subtree := []uint64{id}
	for {
		grew := false
		for _, d := range r.items {
			if d.ParentID == nil {
				continue
			}
			for _, s := range subtree {
				if *d.ParentID == s && !containsID(subtree, d.ID) {
					subtree = append(subtree, d.ID)
					grew = true
				}
			}
		}
		if !grew {
			break
		}
	}
	r.mu.Unlock()

	if r.empRef != nil {
		r.empRef.reassign(subtree, target)
	}
	return r.Delete(context.Background(), id)
}

func (r *fakeDeptRepo) NameExistsInParent(_ context.Context, parentID *uint64, name string, exclude uint64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.items {
		if d.ID == exclude {
			continue
		}
		if d.Name == name && samePtr(d.ParentID, parentID) {
			return true, nil
		}
	}
	return false, nil
}

// fakeEmpRepo is an in-memory EmployeeRepo.
type fakeEmpRepo struct {
	mu     sync.Mutex
	nextID uint64
	items  map[uint64]*domain.Employee
}

func newFakeEmpRepo() *fakeEmpRepo {
	return &fakeEmpRepo{items: map[uint64]*domain.Employee{}}
}

func (r *fakeEmpRepo) Create(_ context.Context, e *domain.Employee) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	e.ID = r.nextID
	e.CreatedAt = time.Now()
	clone := *e
	r.items[e.ID] = &clone
	return nil
}

func (r *fakeEmpRepo) ListByDepartmentIDs(
	_ context.Context,
	ids []uint64,
	sortBy domain.EmployeeSortField,
) ([]domain.Employee, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	set := map[uint64]bool{}
	for _, id := range ids {
		set[id] = true
	}
	var out []domain.Employee
	for _, e := range r.items {
		if set[e.DepartmentID] {
			out = append(out, *e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if sortBy == domain.SortEmployeesByFullName {
			if c := strings.Compare(out[i].FullName, out[j].FullName); c != 0 {
				return c < 0
			}
			return out[i].ID < out[j].ID
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (r *fakeEmpRepo) deleteByDept(ids map[uint64]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, e := range r.items {
		if ids[e.DepartmentID] {
			delete(r.items, id)
		}
	}
}

func (r *fakeEmpRepo) reassign(deptIDs []uint64, target uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	set := map[uint64]bool{}
	for _, id := range deptIDs {
		set[id] = true
	}
	for _, e := range r.items {
		if set[e.DepartmentID] {
			e.DepartmentID = target
		}
	}
}

// helpers
func samePtr(a, b *uint64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func containsID(s []uint64, v uint64) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
