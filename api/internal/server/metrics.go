package server

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// metrics is a minimal, dependency-free Prometheus text-format exporter —
// just enough for request/error/rate-limit counters without pulling in the
// full client_golang module.
type metrics struct {
	requestsTotal   atomic.Int64
	errorsTotal     atomic.Int64
	rateLimitedHits atomic.Int64
}

func (m *metrics) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# TYPE featherflags_requests_total counter\nfeatherflags_requests_total %d\n", m.requestsTotal.Load())
		fmt.Fprintf(w, "# TYPE featherflags_errors_total counter\nfeatherflags_errors_total %d\n", m.errorsTotal.Load())
		fmt.Fprintf(w, "# TYPE featherflags_rate_limited_total counter\nfeatherflags_rate_limited_total %d\n", m.rateLimitedHits.Load())
	}
}

func (m *metrics) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.requestsTotal.Add(1)
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if sw.status >= 500 {
			m.errorsTotal.Add(1)
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
