package api

import (
	"encoding/json"
	"log"
	"net/http"
	"runtime"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/nsilverman/archivist/internal/config"
	"github.com/nsilverman/archivist/internal/executor"
	"github.com/nsilverman/archivist/internal/models"
	"github.com/nsilverman/archivist/internal/scheduler"
	"github.com/nsilverman/archivist/internal/storage"
)

// Server represents the HTTP API server
type Server struct {
	config    *config.Manager
	db        *storage.Database
	executor  *executor.Executor
	scheduler *scheduler.Scheduler
	wsClients map[*websocket.Conn]bool
	wsMu      sync.RWMutex
	upgrader  websocket.Upgrader
}

// Response represents a standard API response
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorInfo  `json:"error,omitempty"`
}

// ErrorInfo represents error information
type ErrorInfo struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

// NewServer creates a new API server
func NewServer(cfg *config.Manager, db *storage.Database, exec *executor.Executor, sched *scheduler.Scheduler) *Server {
	s := &Server{
		config:    cfg,
		db:        db,
		executor:  exec,
		scheduler: sched,
		wsClients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
	}

	// Set executor's progress broadcaster
	exec.SetProgressBroadcaster(s)

	return s
}

// Router returns the HTTP router
func (s *Server) Router() *mux.Router {
	r := mux.NewRouter()

	// Logging middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("%s %s", r.Method, r.URL.Path)
			next.ServeHTTP(w, r)
		})
	})

	// API routes
	api := r.PathPrefix("/api/v1").Subrouter()

	// Tasks
	api.HandleFunc("/tasks", s.listTasks).Methods("GET")
	api.HandleFunc("/tasks", s.createTask).Methods("POST")
	api.HandleFunc("/tasks/{id}", s.getTask).Methods("GET")
	api.HandleFunc("/tasks/{id}", s.updateTask).Methods("PUT")
	api.HandleFunc("/tasks/{id}", s.deleteTask).Methods("DELETE")
	api.HandleFunc("/tasks/{id}/execute", s.executeTask).Methods("POST")
	api.HandleFunc("/tasks/{id}/enable", s.enableTask).Methods("POST")
	api.HandleFunc("/tasks/{id}/disable", s.disableTask).Methods("POST")

	// Backends
	api.HandleFunc("/backends", s.listBackends).Methods("GET")
	api.HandleFunc("/backends", s.createBackend).Methods("POST")
	api.HandleFunc("/backends/{id}", s.getBackend).Methods("GET")
	api.HandleFunc("/backends/{id}", s.updateBackend).Methods("PUT")
	api.HandleFunc("/backends/{id}", s.deleteBackend).Methods("DELETE")
	api.HandleFunc("/backends/{id}/test", s.testBackend).Methods("POST")

	// Executions
	api.HandleFunc("/executions", s.listExecutions).Methods("GET")
	api.HandleFunc("/executions", s.clearHistory).Methods("DELETE")
	api.HandleFunc("/executions/{id}", s.getExecution).Methods("GET")
	api.HandleFunc("/executions/{id}/cancel", s.cancelExecution).Methods("POST")

	// Sources
	api.HandleFunc("/sources", s.listSources).Methods("GET")

	// Configuration
	api.HandleFunc("/config", s.getConfig).Methods("GET")
	api.HandleFunc("/config/settings", s.updateSettings).Methods("PUT")

	// System
	api.HandleFunc("/system/health", s.healthCheck).Methods("GET")
	api.HandleFunc("/system/stats", s.systemStats).Methods("GET")

	// WebSocket
	api.HandleFunc("/ws/progress", s.handleWebSocket)

	// Serve static files
	fs := http.FileServer(http.Dir("./web/static"))
	r.PathPrefix("/css/").Handler(fs)
	r.PathPrefix("/js/").Handler(fs)

	// Serve index.html at root
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./web/static/index.html")
	})

	return r
}

// BroadcastProgress implements executor.ProgressBroadcaster
func (s *Server) BroadcastProgress(event models.ProgressEvent) {
	s.wsMu.RLock()
	defer s.wsMu.RUnlock()

	for client := range s.wsClients {
		if err := client.WriteJSON(event); err != nil {
			// Client disconnected, will be cleaned up
			continue
		}
	}
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	s.wsMu.Lock()
	s.wsClients[conn] = true
	s.wsMu.Unlock()

	defer func() {
		s.wsMu.Lock()
		delete(s.wsClients, conn)
		s.wsMu.Unlock()
		if err := conn.Close(); err != nil {
			log.Printf("Error closing WebSocket connection: %v", err)
		}
	}()

	// Keep connection alive and handle client messages if needed
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

// Helper functions
func (s *Server) success(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(Response{Success: true, Data: data}); err != nil {
		log.Printf("Error encoding success response: %v", err)
	}
}

func (s *Server) error(w http.ResponseWriter, code string, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
	}); err != nil {
		log.Printf("Error encoding error response: %v", err)
	}
}

// Health check
func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
	s.success(w, map[string]interface{}{
		"status":  "healthy",
		"version": "1.0.0-dev",
	})
}

// System stats
func (s *Server) systemStats(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	tasks := s.config.GetTasks()
	backends := s.config.GetBackends()

	enabledTasks := 0
	for _, task := range tasks {
		if task.Enabled {
			enabledTasks++
		}
	}

	enabledBackends := 0
	for _, backend := range backends {
		if backend.Enabled {
			enabledBackends++
		}
	}

	// Get execution statistics from database
	executionStats, err := s.db.GetExecutionStats()
	if err != nil {
		s.error(w, "STATS_ERROR", "Failed to get execution stats", http.StatusInternalServerError)
		return
	}

	stats := models.SystemStats{
		Tasks: models.TasksStats{
			Total:    len(tasks),
			Enabled:  enabledTasks,
			Disabled: len(tasks) - enabledTasks,
		},
		Backends: models.BackendsStats{
			Total:    len(backends),
			Enabled:  enabledBackends,
			Disabled: len(backends) - enabledBackends,
		},
		Executions: *executionStats,
		System: models.SystemInfo{
			MemoryUsed:  int64(m.Alloc),
			MemoryTotal: int64(m.Sys),
			Goroutines:  runtime.NumGoroutine(),
		},
	}

	s.success(w, stats)
}
