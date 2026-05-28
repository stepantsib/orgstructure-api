package http

import (
	"net/http"

	"orgstructure/internal/domain"
	"orgstructure/internal/service"
)

// EmployeeHandler exposes the employee-related endpoints.
type EmployeeHandler struct {
	svc *service.EmployeeService
}

func NewEmployeeHandler(svc *service.EmployeeService) *EmployeeHandler {
	return &EmployeeHandler{svc: svc}
}

// Create handles POST /departments/{id}/employees/.
func (h *EmployeeHandler) Create(w http.ResponseWriter, r *http.Request) {
	deptID, err := parseUint64Path(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}

	var in domain.CreateEmployeeInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	e, err := h.svc.Create(r.Context(), deptID, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}
