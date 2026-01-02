package scheduler

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nsilverman/archivist/internal/config"
	"github.com/nsilverman/archivist/internal/executor"
	"github.com/nsilverman/archivist/internal/models"
	"github.com/robfig/cron/v3"
)

// Scheduler manages task scheduling
type Scheduler struct {
	cron     *cron.Cron
	config   *config.Manager
	executor *executor.Executor
	entries  map[string]cron.EntryID // taskID -> entryID
	mu       sync.RWMutex
}

// NewScheduler creates a new scheduler
func NewScheduler(exec *executor.Executor, cfg *config.Manager) *Scheduler {
	return &Scheduler{
		cron:     cron.New(),
		config:   cfg,
		executor: exec,
		entries:  make(map[string]cron.EntryID),
	}
}

// Start starts the scheduler
func (s *Scheduler) Start() error {
	// Load all tasks and schedule them
	tasks := s.config.GetTasks()
	for _, task := range tasks {
		if task.Enabled && task.Schedule.Type != "manual" {
			if err := s.scheduleTask(&task); err != nil {
				log.Printf("Failed to schedule task %s: %v", task.Name, err)
			}
		}
	}

	s.cron.Start()
	log.Println("Scheduler started")
	return nil
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.cron.Stop()
	log.Println("Scheduler stopped")
}

// ScheduleTask adds or updates a task in the scheduler
func (s *Scheduler) ScheduleTask(taskID string) error {
	task, err := s.config.GetTask(taskID)
	if err != nil {
		return err
	}

	// Remove existing schedule if any
	s.UnscheduleTask(taskID)

	// Schedule if enabled and not manual
	if task.Enabled && task.Schedule.Type != "manual" {
		return s.scheduleTask(task)
	}

	return nil
}

// UnscheduleTask removes a task from the scheduler
func (s *Scheduler) UnscheduleTask(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.entries[taskID]; exists {
		s.cron.Remove(entryID)
		delete(s.entries, taskID)
		log.Printf("Unscheduled task: %s", taskID)
	}
}

// scheduleTask adds a task to the cron scheduler
func (s *Scheduler) scheduleTask(task *models.Task) error {
	// Convert schedule to cron expression
	cronExpr, err := s.scheduleToCron(task.Schedule)
	if err != nil {
		return fmt.Errorf("invalid schedule: %w", err)
	}

	// Add to cron
	entryID, err := s.cron.AddFunc(cronExpr, func() {
		log.Printf("Executing scheduled task: %s", task.Name)
		if _, err := s.executor.Execute(task.ID); err != nil {
			log.Printf("Failed to execute task %s: %v", task.Name, err)
		}
	})

	if err != nil {
		return fmt.Errorf("failed to add task to scheduler: %w", err)
	}

	s.mu.Lock()
	s.entries[task.ID] = entryID
	s.mu.Unlock()

	// Calculate next run time
	entry := s.cron.Entry(entryID)
	nextRun := entry.Next
	if err := s.config.UpdateTaskSchedule(task.ID, nil, &nextRun); err != nil {
		log.Printf("Warning: failed to update task schedule: %v", err)
	}

	log.Printf("Scheduled task %s with expression: %s (next run: %s)", task.Name, cronExpr, nextRun.Format(time.RFC3339))
	return nil
}

// scheduleToCron converts a Schedule to a cron expression
func (s *Scheduler) scheduleToCron(schedule models.Schedule) (string, error) {
	switch schedule.Type {
	case "simple":
		return s.simpleScheduleToCron(schedule.SimpleType)
	case "cron":
		if schedule.CronExpr == "" {
			return "", fmt.Errorf("cron expression is empty")
		}
		return schedule.CronExpr, nil
	case "manual":
		return "", fmt.Errorf("manual tasks cannot be scheduled")
	default:
		return "", fmt.Errorf("unknown schedule type: %s", schedule.Type)
	}
}

// simpleScheduleToCron converts simple schedule types to cron expressions
func (s *Scheduler) simpleScheduleToCron(simpleType string) (string, error) {
	switch simpleType {
	case "hourly":
		return "0 * * * *", nil // Every hour at minute 0
	case "daily":
		return "0 2 * * *", nil // Every day at 2:00 AM
	case "weekly":
		return "0 2 * * 0", nil // Every Sunday at 2:00 AM
	case "monthly":
		return "0 2 1 * *", nil // First day of every month at 2:00 AM
	default:
		return "", fmt.Errorf("unknown simple schedule type: %s", simpleType)
	}
}

// GetNextRun returns the next scheduled run time for a task
func (s *Scheduler) GetNextRun(taskID string) (*time.Time, error) {
	s.mu.RLock()
	entryID, exists := s.entries[taskID]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("task not scheduled")
	}

	entry := s.cron.Entry(entryID)
	next := entry.Next
	return &next, nil
}

// ReloadSchedules reloads all task schedules from configuration
func (s *Scheduler) ReloadSchedules() error {
	log.Println("Reloading task schedules...")

	// Clear all existing schedules
	s.mu.Lock()
	for taskID := range s.entries {
		s.cron.Remove(s.entries[taskID])
		delete(s.entries, taskID)
	}
	s.mu.Unlock()

	// Load and schedule all tasks
	tasks := s.config.GetTasks()
	var errors []error
	for _, task := range tasks {
		if task.Enabled && task.Schedule.Type != "manual" {
			if err := s.scheduleTask(&task); err != nil {
				log.Printf("Failed to schedule task %s: %v", task.Name, err)
				errors = append(errors, err)
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to schedule %d task(s)", len(errors))
	}

	log.Printf("Successfully scheduled %d task(s)", len(s.entries))
	return nil
}
