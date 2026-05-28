package http

import (
	"net/http"
	"strconv"

	"orgstructure/internal/domain"
	"orgstructure/internal/errs"
	"orgstructure/internal/service"
)

// DepartmentHandler exposes the department-related endpoints.
type DepartmentHandler struct {
	svc *service.DepartmentService
}

func NewDepartmentHandler(svc *service.DepartmentService) *DepartmentHandler {
	return &DepartmentHandler{svc: svc}
}

// Create handles POST /departments/.
func (h *DepartmentHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in domain.CreateDepartmentInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	d, err := h.svc.Create(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// Get handles GET /departments/{id}?depth=&include_employees=&sort_employees_by=.
func (h *DepartmentHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUint64Path(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	depth := 1
	if raw := r.URL.Query().Get("depth"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			writeError(w, errs.ErrBadRequest)
			return
		}
		depth = v
	}
	if depth > service.MaxTreeDepth {
		depth = service.MaxTreeDepth
	}

	includeEmps := true
	if raw := r.URL.Query().Get("include_employees"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, errs.ErrBadRequest)
			return
		}
		includeEmps = v
	}

	sortBy := domain.SortEmployeesByCreatedAt
	if raw := r.URL.Query().Get("sort_employees_by"); raw != "" {
		s := domain.EmployeeSortField(raw)
		if !s.Valid() {
			writeError(w, errs.ErrBadRequest)
			return
		}
		sortBy = s
	}

	tree, err := h.svc.GetTree(r.Context(), id, depth, includeEmps, sortBy)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

// Update handles PATCH /departments/{id}.
func (h *DepartmentHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUint64Path(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	var in domain.UpdateDepartmentInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	d, err := h.svc.Update(r.Context(), id, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// Delete handles DELETE /departments/{id}?mode=&reassign_to_department_id=.
func (h *DepartmentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUint64Path(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	mode := domain.DeleteMode(r.URL.Query().Get("mode"))
	if !mode.Valid() {
		writeError(w, errs.ErrBadRequest)
		return
	}

	var reassignTo *uint64
	if raw := r.URL.Query().Get("reassign_to_department_id"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			writeError(w, errs.ErrBadRequest)
			return
		}
		reassignTo = &v
	}

	if err := h.svc.Delete(r.Context(), id, mode, reassignTo); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseUint64Path reads {name} from the request path and parses it as uint64.
// Bad input returns errs.ErrBadRequest so the caller can pass it to writeError.
func parseUint64Path(r *http.Request, name string) (uint64, error) {
	raw := r.PathValue(name)
	if raw == "" {
		return 0, errs.ErrBadRequest
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || v == 0 {
		return 0, errs.ErrBadRequest
	}
	return v, nil
}
