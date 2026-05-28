package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"orgstructure/internal/domain"
	"orgstructure/internal/errs"
	"orgstructure/internal/service"
)

// The HTTP tests exercise the full transport → service stack with in-memory
// fake repositories. They verify status codes, response shapes, and that
// errors propagate to the correct HTTP class (4xx/5xx).

// --- Fakes (kept package-local so they can share the fake state) ------------

type memDeptRepo struct {
	mu     sync.Mutex
	next   uint64
	items  map[uint64]*domain.Department
	empRef *memEmpRepo
}

func newMemDeptRepo() *memDeptRepo {
	return &memDeptRepo{items: map[uint64]*domain.Department{}}
}

func (r *memDeptRepo) Create(_ context.Context, d *domain.Department) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.items {
		if e.Name == d.Name && samePtr(e.ParentID, d.ParentID) {
			return errs.ErrConflict
		}
	}
	r.next++
	d.ID = r.next
	d.CreatedAt = time.Now()
	c := *d
	r.items[d.ID] = &c
	return nil
}

func (r *memDeptRepo) GetByID(_ context.Context, id uint64) (*domain.Department, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.items[id]
	if !ok {
		return nil, errs.ErrNotFound
	}
	c := *d
	return &c, nil
}

func (r *memDeptRepo) Exists(_ context.Context, id uint64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.items[id]
	return ok, nil
}

func (r *memDeptRepo) Update(_ context.Context, id uint64, updates map[string]any) (*domain.Department, error) {
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

func (r *memDeptRepo) SubtreeIDs(_ context.Context, rootID uint64) ([]uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[rootID]; !ok {
		return nil, nil
	}
	out := []uint64{rootID}
	frontier := []uint64{rootID}
	for len(frontier) > 0 {
		nxt := []uint64{}
		for _, d := range r.items {
			if d.ParentID == nil {
				continue
			}
			for _, f := range frontier {
				if *d.ParentID == f {
					out = append(out, d.ID)
					nxt = append(nxt, d.ID)
				}
			}
		}
		frontier = nxt
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (r *memDeptRepo) SubtreeDepartments(_ context.Context, rootID uint64, depth int) ([]domain.Department, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	root, ok := r.items[rootID]
	if !ok {
		return nil, nil
	}
	type lvl struct {
		d domain.Department
		n int
	}
	out := []lvl{{d: *root, n: 0}}
	frontier := []uint64{rootID}
	for level := 1; level <= depth && len(frontier) > 0; level++ {
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
					out = append(out, lvl{d: *d, n: level})
					next = append(next, d.ID)
				}
			}
		}
		frontier = next
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n < out[j].n
		}
		return out[i].d.ID < out[j].d.ID
	})
	res := make([]domain.Department, len(out))
	for i, v := range out {
		res[i] = v.d
	}
	return res, nil
}

func (r *memDeptRepo) Delete(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return errs.ErrNotFound
	}
	remove := map[uint64]bool{id: true}
	for {
		grew := false
		for _, d := range r.items {
			if d.ParentID != nil && remove[*d.ParentID] && !remove[d.ID] {
				remove[d.ID] = true
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	for id := range remove {
		delete(r.items, id)
	}
	if r.empRef != nil {
		r.empRef.deleteByDept(remove)
	}
	return nil
}

func (r *memDeptRepo) ReassignAndDelete(_ context.Context, id, target uint64) error {
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

func (r *memDeptRepo) NameExistsInParent(_ context.Context, parentID *uint64, name string, exclude uint64) (bool, error) {
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

type memEmpRepo struct {
	mu    sync.Mutex
	next  uint64
	items map[uint64]*domain.Employee
}

func newMemEmpRepo() *memEmpRepo {
	return &memEmpRepo{items: map[uint64]*domain.Employee{}}
}

func (r *memEmpRepo) Create(_ context.Context, e *domain.Employee) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	e.ID = r.next
	e.CreatedAt = time.Now()
	c := *e
	r.items[e.ID] = &c
	return nil
}

func (r *memEmpRepo) ListByDepartmentIDs(_ context.Context, ids []uint64, sortBy domain.EmployeeSortField) ([]domain.Employee, error) {
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

func (r *memEmpRepo) deleteByDept(ids map[uint64]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, e := range r.items {
		if ids[e.DepartmentID] {
			delete(r.items, id)
		}
	}
}

func (r *memEmpRepo) reassign(deptIDs []uint64, target uint64) {
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

// --- Test harness ----------------------------------------------------------

func newTestServer(t *testing.T) (*httptest.Server, *memDeptRepo, *memEmpRepo) {
	t.Helper()
	dRepo := newMemDeptRepo()
	eRepo := newMemEmpRepo()
	dRepo.empRef = eRepo
	svcs := service.New(dRepo, eRepo)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(NewRouter(svcs, logger))
	t.Cleanup(srv.Close)
	return srv, dRepo, eRepo
}

func doJSON(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reader)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var out T
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}

// --- Tests -----------------------------------------------------------------

func TestHTTP_CreateDepartment(t *testing.T) {
	srv, _, _ := newTestServer(t)

	resp := doJSON(t, http.MethodPost, srv.URL+"/departments/",
		map[string]any{"name": "Engineering"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	d := decode[domain.Department](t, resp)
	assert.Equal(t, "Engineering", d.Name)
	assert.NotZero(t, d.ID)
}

func TestHTTP_CreateDepartment_Validation_422(t *testing.T) {
	srv, _, _ := newTestServer(t)

	resp := doJSON(t, http.MethodPost, srv.URL+"/departments/",
		map[string]any{"name": "   "})
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

	body := decode[map[string]any](t, resp)
	assert.Equal(t, "validation_error", body["code"])
}

func TestHTTP_GetDepartment_TreeWithDepth(t *testing.T) {
	srv, _, _ := newTestServer(t)

	// Build A -> B -> C.
	resp := doJSON(t, http.MethodPost, srv.URL+"/departments/", map[string]any{"name": "A"})
	a := decode[domain.Department](t, resp)

	resp = doJSON(t, http.MethodPost, srv.URL+"/departments/",
		map[string]any{"name": "B", "parent_id": a.ID})
	b := decode[domain.Department](t, resp)

	resp = doJSON(t, http.MethodPost, srv.URL+"/departments/",
		map[string]any{"name": "C", "parent_id": b.ID})
	_ = decode[domain.Department](t, resp)

	// depth=1 → A → B, no C.
	resp, err := http.Get(srv.URL + "/departments/" + itoa(a.ID) + "?depth=1&include_employees=false")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	tree := decode[domain.DepartmentTreeNode](t, resp)
	require.Len(t, tree.Children, 1)
	assert.Equal(t, "B", tree.Children[0].Department.Name)
	assert.Empty(t, tree.Children[0].Children)

	// depth=2 → also C.
	resp, err = http.Get(srv.URL + "/departments/" + itoa(a.ID) + "?depth=2&include_employees=false")
	require.NoError(t, err)
	tree = decode[domain.DepartmentTreeNode](t, resp)
	require.Len(t, tree.Children, 1)
	require.Len(t, tree.Children[0].Children, 1)
	assert.Equal(t, "C", tree.Children[0].Children[0].Department.Name)
}

func TestHTTP_PatchDepartment_CycleConflict(t *testing.T) {
	srv, _, _ := newTestServer(t)

	resp := doJSON(t, http.MethodPost, srv.URL+"/departments/", map[string]any{"name": "A"})
	a := decode[domain.Department](t, resp)
	resp = doJSON(t, http.MethodPost, srv.URL+"/departments/",
		map[string]any{"name": "B", "parent_id": a.ID})
	b := decode[domain.Department](t, resp)

	// Trying to make A a child of B (its descendant) → 409.
	resp = doJSON(t, http.MethodPatch, srv.URL+"/departments/"+itoa(a.ID),
		map[string]any{"parent_id": b.ID})
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	resp.Body.Close()
}

func TestHTTP_PatchDepartment_PromoteToRootWithNull(t *testing.T) {
	srv, _, _ := newTestServer(t)
	resp := doJSON(t, http.MethodPost, srv.URL+"/departments/", map[string]any{"name": "A"})
	a := decode[domain.Department](t, resp)
	resp = doJSON(t, http.MethodPost, srv.URL+"/departments/",
		map[string]any{"name": "B", "parent_id": a.ID})
	b := decode[domain.Department](t, resp)

	resp = doJSON(t, http.MethodPatch, srv.URL+"/departments/"+itoa(b.ID),
		map[string]any{"parent_id": nil})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	updated := decode[domain.Department](t, resp)
	assert.Nil(t, updated.ParentID)
}

func TestHTTP_DeleteDepartment_Cascade_204(t *testing.T) {
	srv, _, _ := newTestServer(t)
	resp := doJSON(t, http.MethodPost, srv.URL+"/departments/", map[string]any{"name": "A"})
	a := decode[domain.Department](t, resp)

	req, _ := http.NewRequest(http.MethodDelete,
		srv.URL+"/departments/"+itoa(a.ID)+"?mode=cascade", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp, err = http.Get(srv.URL + "/departments/" + itoa(a.ID))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHTTP_DeleteDepartment_Reassign_MovesEmployees(t *testing.T) {
	srv, _, _ := newTestServer(t)

	resp := doJSON(t, http.MethodPost, srv.URL+"/departments/", map[string]any{"name": "Src"})
	src := decode[domain.Department](t, resp)
	resp = doJSON(t, http.MethodPost, srv.URL+"/departments/", map[string]any{"name": "Tgt"})
	tgt := decode[domain.Department](t, resp)

	resp = doJSON(t, http.MethodPost, srv.URL+"/departments/"+itoa(src.ID)+"/employees/",
		map[string]any{"full_name": "Alice", "position": "Engineer"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete,
		srv.URL+"/departments/"+itoa(src.ID)+"?mode=reassign&reassign_to_department_id="+itoa(tgt.ID),
		nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp, err = http.Get(srv.URL + "/departments/" + itoa(tgt.ID) + "?include_employees=true")
	require.NoError(t, err)
	tree := decode[domain.DepartmentTreeNode](t, resp)
	require.NotNil(t, tree.Employees)
	assert.Len(t, *tree.Employees, 1)
	assert.Equal(t, "Alice", (*tree.Employees)[0].FullName)
}

func TestHTTP_CreateEmployee_MissingDepartment_404(t *testing.T) {
	srv, _, _ := newTestServer(t)
	resp := doJSON(t, http.MethodPost, srv.URL+"/departments/999/employees/",
		map[string]any{"full_name": "Alice", "position": "Engineer"})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHTTP_DepartmentNameConflict_409(t *testing.T) {
	srv, _, _ := newTestServer(t)

	resp := doJSON(t, http.MethodPost, srv.URL+"/departments/", map[string]any{"name": "X"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	resp = doJSON(t, http.MethodPost, srv.URL+"/departments/", map[string]any{"name": "X"})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestHTTP_Healthz(t *testing.T) {
	srv, _, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// itoa formats a uint64 without pulling in strconv at the call site.
func itoa(v uint64) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v%10]
		v /= 10
	}
	return string(buf[i:])
}
