package http

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder captures the response status so the logging middleware can
// include it in the access log line.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// LoggingMiddleware emits one structured log line per request, with method,
// path, status, byte count, and duration. Panics are recovered and turned
// into 500 responses to keep the server alive.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			defer func() {
				if rec.status == 0 {
					rec.status = http.StatusOK
				}
				if rcv := recover(); rcv != nil {
					logger.Error("panic recovered",
						"error", rcv,
						"method", r.Method,
						"path", r.URL.Path,
					)
					if rec.status == 0 || rec.status == http.StatusOK {
						http.Error(rec, "internal server error", http.StatusInternalServerError)
					}
				}
				logger.Info("http request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", rec.status,
					"bytes", rec.bytes,
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}()

			next.ServeHTTP(rec, r)
		})
	}
}
