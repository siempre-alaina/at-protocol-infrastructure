package health

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// Status represents the health status of a component
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// CheckResult represents the result of a health check
type CheckResult struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Response represents the health check response
type Response struct {
	Status    Status                 `json:"status"`
	Version   string                 `json:"version"`
	Uptime    string                 `json:"uptime"`
	Checks    map[string]CheckResult `json:"checks"`
	Timestamp time.Time              `json:"timestamp"`
}

// Checker is a function that performs a health check
type Checker func() CheckResult

// Handler manages health checks
type Handler struct {
	version   string
	startTime time.Time
	checks    map[string]Checker
	ready     atomic.Bool
}

// New creates a new health handler
func New(version string) *Handler {
	h := &Handler{
		version:   version,
		startTime: time.Now(),
		checks:    make(map[string]Checker),
	}
	h.ready.Store(false)
	return h
}

// RegisterCheck adds a health check
func (h *Handler) RegisterCheck(name string, checker Checker) {
	h.checks[name] = checker
}

// SetReady marks the service as ready
func (h *Handler) SetReady(ready bool) {
	h.ready.Store(ready)
}

// IsReady returns whether the service is ready
func (h *Handler) IsReady() bool {
	return h.ready.Load()
}

// HealthHandler returns an HTTP handler for health checks
func (h *Handler) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := h.runChecks()

		w.Header().Set("Content-Type", "application/json")

		if response.Status == StatusUnhealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else if response.Status == StatusDegraded {
			w.WriteHeader(http.StatusOK) // Still return 200 for degraded
		} else {
			w.WriteHeader(http.StatusOK)
		}

		json.NewEncoder(w).Encode(response)
	}
}

// ReadyHandler returns an HTTP handler for readiness checks
func (h *Handler) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.IsReady() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ready"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("not ready"))
		}
	}
}

// LiveHandler returns an HTTP handler for liveness checks
func (h *Handler) LiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("alive"))
	}
}

func (h *Handler) runChecks() Response {
	results := make(map[string]CheckResult)
	overallStatus := StatusHealthy

	for name, checker := range h.checks {
		result := checker()
		results[name] = result

		// Determine overall status (worst wins)
		if result.Status == StatusUnhealthy {
			overallStatus = StatusUnhealthy
		} else if result.Status == StatusDegraded && overallStatus != StatusUnhealthy {
			overallStatus = StatusDegraded
		}
	}

	return Response{
		Status:    overallStatus,
		Version:   h.version,
		Uptime:    time.Since(h.startTime).Round(time.Second).String(),
		Checks:    results,
		Timestamp: time.Now().UTC(),
	}
}
