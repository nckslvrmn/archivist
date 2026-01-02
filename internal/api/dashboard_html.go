package api

import (
	"log"
	"net/http"

	"github.com/nsilverman/archivist/internal/models"
)

// dashboardHTML handles GET /api/v1/dashboard/html
func (s *Server) dashboardHTML(w http.ResponseWriter, r *http.Request) {
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

	// Get execution statistics (use defaults if error)
	executionStats, err := s.db.GetExecutionStats()
	if err != nil {
		log.Printf("Failed to get execution stats: %v", err)
		executionStats = &models.ExecutionsStats{
			Total:   0,
			Success: 0,
			Failed:  0,
			Running: 0,
			Last24h: 0,
		}
	}

	// Get recent activity
	recentExecutions, err := s.db.ListExecutions("", "", 10, 0)
	if err != nil {
		log.Printf("Failed to get recent executions: %v", err)
		recentExecutions = nil
	}

	data := map[string]interface{}{
		"TotalTasks":       len(tasks),
		"EnabledTasks":     enabledTasks,
		"TotalBackends":    len(backends),
		"EnabledBackends":  enabledBackends,
		"ExecutionStats":   executionStats,
		"RecentExecutions": recentExecutions,
	}

	s.htmlResponse(w, "dashboard.html", data)
}
