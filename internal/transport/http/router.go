package http

import (
	"log/slog"
	"net/http"

	"orgstructure/internal/service"
)

// NewRouter wires every endpoint into Go 1.22's pattern-based ServeMux and
// wraps the result with the logging/recovery middleware.
//
// Trailing slashes on the resource paths follow the spec verbatim
// (POST /departments/, POST /departments/{id}/employees/).
func NewRouter(svcs *service.Services, logger *slog.Logger) http.Handler {
	dept := NewDepartmentHandler(svcs.Departments)
	emp := NewEmployeeHandler(svcs.Employees)

	mux := http.NewServeMux()

	// Departments
	mux.HandleFunc("POST /departments/", dept.Create)
	mux.HandleFunc("GET /departments/{id}", dept.Get)
	mux.HandleFunc("PATCH /departments/{id}", dept.Update)
	mux.HandleFunc("DELETE /departments/{id}", dept.Delete)

	// Employees (nested under a department)
	mux.HandleFunc("POST /departments/{id}/employees/", emp.Create)

	// Health probe
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return LoggingMiddleware(logger)(mux)
}
