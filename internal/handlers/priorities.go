package handlers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/tech-arch1tect/kan-mcp/internal/api"
	"github.com/tech-arch1tect/kan-mcp/internal/auth"
	"github.com/tech-arch1tect/kan-mcp/internal/models"
)

type PrioritiesHandler struct {
	authManager *auth.AuthManager
	config      *models.UserConfig
}

func NewPrioritiesHandler(authManager *auth.AuthManager, config *models.UserConfig) *PrioritiesHandler {
	return &PrioritiesHandler{
		authManager: authManager,
		config:      config,
	}
}

type PrioritiesRequest struct {
	UserID                 string   `json:"user_id"`
	ProjectIDs             []string `json:"project_ids"`
	TimeHorizon            string   `json:"time_horizon"`
	IncludeRecommendations bool     `json:"include_recommendations"`
}

type UserWorkload struct {
	UserID              string  `json:"user_id"`
	Username            string  `json:"username"`
	Name                string  `json:"name"`
	AssignedTasks       int     `json:"assigned_tasks"`
	OverdueTasks        int     `json:"overdue_tasks"`
	TotalEstimatedHours float64 `json:"total_estimated_hours"`
	CapacityUtilization string  `json:"capacity_utilization"`
	Status              string  `json:"status"`
}

type UrgentItem struct {
	TaskID       string `json:"task_id"`
	Title        string `json:"title"`
	UrgencyScore int    `json:"urgency_score"`
	Reason       string `json:"reason"`
	Project      string `json:"project"`
	DaysOverdue  int    `json:"days_overdue,omitempty"`
}

type Bottleneck struct {
	Column          string   `json:"column"`
	Project         string   `json:"project"`
	StuckTasks      int      `json:"stuck_tasks"`
	AvgWaitTimeDays float64  `json:"avg_wait_time_days"`
	TaskIDs         []string `json:"task_ids"`
}

type Recommendation struct {
	Type              string   `json:"type"`
	Message           string   `json:"message"`
	TaskIDs           []string `json:"task_ids,omitempty"`
	SuggestedAssignee string   `json:"suggested_assignee,omitempty"`
	AffectedTasks     []string `json:"affected_tasks,omitempty"`
	Confidence        float64  `json:"confidence"`
}

type PrioritiesAnalysis struct {
	RequestingUser *UserWorkload  `json:"requesting_user,omitempty"`
	TeamWorkloads  []UserWorkload `json:"team_workloads"`
	UrgentItems    []UrgentItem   `json:"urgent_items"`
	Bottlenecks    []Bottleneck   `json:"bottlenecks"`
}

type PrioritiesResponse struct {
	Analysis        PrioritiesAnalysis `json:"analysis"`
	Recommendations []Recommendation   `json:"recommendations,omitempty"`
}

func (h *PrioritiesHandler) Handle(params map[string]interface{}, userID string) (*models.MCPResponse, error) {
	var req PrioritiesRequest
	req.TimeHorizon = "week"
	req.IncludeRecommendations = true

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, fmt.Errorf("failed to parse priorities request: %w", err)
		}
	}

	if req.UserID == "" {
		req.UserID = userID
	}

	user, err := h.authManager.AuthenticateUser(userID)
	if err == nil {
		token, err := h.authManager.GetDecryptedToken(user)
		if err == nil {
			kanboardURL := user.KanboardURL
			if kanboardURL == "" {
				kanboardURL = h.config.DefaultKanboardURL
			}

			client := api.NewClient(kanboardURL, user.KanboardUsername, token)
			if me, err := client.GetMe(); err == nil {
				req.UserID = fmt.Sprintf("%d", me.ID)
			}
		}
	}

	tasksHandler := NewTasksHandler(h.authManager, h.config)
	tasksParams := map[string]interface{}{
		"project_ids":           req.ProjectIDs,
		"status_filter":         "all",
		"include_overdue":       true,
		"include_time_tracking": true,
		"sort_by":               "due_date",
		"limit":                 200,
		"summary_mode":          false,
	}

	tasksResponse, err := tasksHandler.Handle(tasksParams, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks data: %w", err)
	}

	var tasksData TasksResponse
	if err := json.Unmarshal([]byte(tasksResponse.Content[0].Text), &tasksData); err != nil {
		return nil, fmt.Errorf("failed to parse tasks response: %w", err)
	}

	analysis := h.analyseWorkload(tasksData.Tasks, req)

	var response PrioritiesResponse
	response.Analysis = analysis

	if req.IncludeRecommendations {
		response.Recommendations = h.generateRecommendations(analysis, tasksData.Tasks)
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal priorities response: %w", err)
	}

	return &models.MCPResponse{
		Content: []models.MCPContent{
			{
				Type: "text",
				Text: string(responseJSON),
			},
		},
	}, nil
}

func (h *PrioritiesHandler) analyseWorkload(tasks []TaskDetail, req PrioritiesRequest) PrioritiesAnalysis {
	analysis := PrioritiesAnalysis{
		UrgentItems: []UrgentItem{},
		Bottlenecks: []Bottleneck{},
	}

	analysis.TeamWorkloads = h.analyseTeamWorkloads(tasks)

	for i, workload := range analysis.TeamWorkloads {
		if h.matchesUserID(workload.UserID, req.UserID) {

			requestingUser := analysis.TeamWorkloads[i]
			analysis.RequestingUser = &requestingUser
			break
		}
	}

	analysis.UrgentItems = h.findUrgentItems(tasks, req.TimeHorizon)

	analysis.Bottlenecks = h.findBottlenecks(tasks)

	return analysis
}

func (h *PrioritiesHandler) analyseTeamWorkloads(tasks []TaskDetail) []UserWorkload {
	userMap := make(map[string]*UserWorkload)

	allAssigneeIDs := make([]string, 0)

	for _, task := range tasks {
		if task.Assignee == nil {
			continue
		}

		userID := task.Assignee.ID
		allAssigneeIDs = append(allAssigneeIDs, userID)

		if _, exists := userMap[userID]; !exists {
			userMap[userID] = &UserWorkload{
				UserID:   userID,
				Username: task.Assignee.Username,
				Name:     task.Assignee.Name,
			}
		}

		workload := userMap[userID]
		workload.AssignedTasks++

		if task.IsOverdue {
			workload.OverdueTasks++
		}

		if task.TimeTracking != nil {
			workload.TotalEstimatedHours += task.TimeTracking.EstimatedHours
		}
	}

	var workloads []UserWorkload
	for _, workload := range userMap {
		weeklyCapacity := 40.0
		utilization := (workload.TotalEstimatedHours / weeklyCapacity) * 100
		workload.CapacityUtilization = fmt.Sprintf("%.0f%%", utilization)

		if utilization > 120 {
			workload.Status = "severely_overloaded"
		} else if utilization > 100 {
			workload.Status = "overloaded"
		} else if utilization > 80 {
			workload.Status = "at_capacity"
		} else if utilization > 50 {
			workload.Status = "normal"
		} else {
			workload.Status = "underutilized"
		}

		workloads = append(workloads, *workload)
	}

	if len(workloads) == 0 && len(allAssigneeIDs) > 0 {

		uniqueIDs := make(map[string]bool)
		for _, id := range allAssigneeIDs {
			uniqueIDs[id] = true
		}
		debugIDs := make([]string, 0, len(uniqueIDs))
		for id := range uniqueIDs {
			debugIDs = append(debugIDs, id)
		}

		workloads = append(workloads, UserWorkload{
			UserID:   "debug",
			Username: fmt.Sprintf("DEBUG: Found %d tasks with assignee IDs: %v", len(allAssigneeIDs), debugIDs),
			Name:     "Debug Info",
		})
	}

	sort.Slice(workloads, func(i, j int) bool {
		return workloads[i].TotalEstimatedHours > workloads[j].TotalEstimatedHours
	})

	return workloads
}

func (h *PrioritiesHandler) findUrgentItems(tasks []TaskDetail, timeHorizon string) []UrgentItem {
	var urgentItems []UrgentItem
	now := time.Now()

	var timeLimit time.Time
	switch timeHorizon {
	case "today":
		timeLimit = now.AddDate(0, 0, 1)
	case "week":
		timeLimit = now.AddDate(0, 0, 7)
	case "month":
		timeLimit = now.AddDate(0, 1, 0)
	default:
		timeLimit = now.AddDate(0, 0, 7)
	}

	for _, task := range tasks {
		urgencyScore := h.calculateUrgencyScore(task, now, timeLimit)
		if urgencyScore >= 70 {
			item := UrgentItem{
				TaskID:       task.ID,
				Title:        task.Title,
				UrgencyScore: urgencyScore,
				Project:      task.Project.Name,
				Reason:       h.getUrgencyReason(task, now),
			}

			if task.IsOverdue && task.DaysUntilDue != nil {
				item.DaysOverdue = -*task.DaysUntilDue
			}

			urgentItems = append(urgentItems, item)
		}
	}

	sort.Slice(urgentItems, func(i, j int) bool {
		return urgentItems[i].UrgencyScore > urgentItems[j].UrgencyScore
	})

	if len(urgentItems) > 10 {
		urgentItems = urgentItems[:10]
	}

	return urgentItems
}

func (h *PrioritiesHandler) calculateUrgencyScore(task TaskDetail, now, timeLimit time.Time) int {
	score := 0

	if task.Dates.Due != "" {
		score += 20
	}

	if task.IsOverdue {
		score += 40
		if task.DaysUntilDue != nil {
			daysOverdue := -*task.DaysUntilDue
			if daysOverdue > 7 {
				score += 30
			} else if daysOverdue > 3 {
				score += 20
			} else {
				score += 10
			}
		}
	}

	if !task.IsOverdue && task.Dates.Due != "" {
		if dueDate, err := time.Parse("2006-01-02T15:04:05Z", task.Dates.Due); err == nil {
			if dueDate.Before(timeLimit) {
				daysUntil := int(dueDate.Sub(now).Hours() / 24)
				if daysUntil <= 1 {
					score += 25
				} else if daysUntil <= 3 {
					score += 15
				} else if daysUntil <= 7 {
					score += 10
				}
			}
		}
	}

	switch task.Priority {
	case "urgent":
		score += 25
	case "high":
		score += 15
	case "normal":
		score += 5
	}

	if task.Assignee == nil {
		score += 15
	}

	return score
}

func (h *PrioritiesHandler) getUrgencyReason(task TaskDetail, now time.Time) string {
	reasons := []string{}

	if task.IsOverdue {
		if task.DaysUntilDue != nil {
			daysOverdue := -*task.DaysUntilDue
			if daysOverdue == 1 {
				reasons = append(reasons, "Overdue by 1 day")
			} else {
				reasons = append(reasons, fmt.Sprintf("Overdue by %d days", daysOverdue))
			}
		} else {
			reasons = append(reasons, "Task is overdue")
		}
	} else if task.Dates.Due != "" {
		if dueDate, err := time.Parse("2006-01-02T15:04:05Z", task.Dates.Due); err == nil {
			daysUntil := int(dueDate.Sub(now).Hours() / 24)
			if daysUntil == 0 {
				reasons = append(reasons, "Due today")
			} else if daysUntil == 1 {
				reasons = append(reasons, "Due tomorrow")
			} else if daysUntil <= 3 {
				reasons = append(reasons, fmt.Sprintf("Due in %d days", daysUntil))
			}
		}
	}

	if task.Priority == "urgent" {
		reasons = append(reasons, "marked as urgent priority")
	} else if task.Priority == "high" {
		reasons = append(reasons, "marked as high priority")
	}

	if task.Assignee == nil {
		reasons = append(reasons, "unassigned task needs attention")
	}

	if len(reasons) == 0 {
		return "High priority task"
	}

	return fmt.Sprintf("%s", reasons[0])
}

func (h *PrioritiesHandler) findBottlenecks(tasks []TaskDetail) []Bottleneck {

	columnStats := make(map[string]map[string][]TaskDetail)

	for _, task := range tasks {
		projectKey := task.Project.Name
		columnKey := task.Status.Column

		if columnStats[projectKey] == nil {
			columnStats[projectKey] = make(map[string][]TaskDetail)
		}

		columnStats[projectKey][columnKey] = append(columnStats[projectKey][columnKey], task)
	}

	var bottlenecks []Bottleneck
	now := time.Now()

	for project, columns := range columnStats {
		for column, columnTasks := range columns {
			if len(columnTasks) < 3 {
				continue
			}

			var totalWaitDays float64
			var validTasks int
			var taskIDs []string

			for _, task := range columnTasks {
				if task.Dates.Modified != "" {
					if modifiedDate, err := time.Parse("2006-01-02T15:04:05Z", task.Dates.Modified); err == nil {
						waitDays := now.Sub(modifiedDate).Hours() / 24
						if waitDays > 2 {
							totalWaitDays += waitDays
							validTasks++
							taskIDs = append(taskIDs, task.ID)
						}
					}
				}
			}

			if validTasks >= 3 {
				avgWaitTime := totalWaitDays / float64(validTasks)
				if avgWaitTime > 3 {
					bottleneck := Bottleneck{
						Column:          column,
						Project:         project,
						StuckTasks:      validTasks,
						AvgWaitTimeDays: avgWaitTime,
						TaskIDs:         taskIDs,
					}
					bottlenecks = append(bottlenecks, bottleneck)
				}
			}
		}
	}

	sort.Slice(bottlenecks, func(i, j int) bool {
		return bottlenecks[i].AvgWaitTimeDays > bottlenecks[j].AvgWaitTimeDays
	})

	return bottlenecks
}

func (h *PrioritiesHandler) generateRecommendations(analysis PrioritiesAnalysis, tasks []TaskDetail) []Recommendation {
	var recommendations []Recommendation

	if len(analysis.UrgentItems) > 0 {
		topUrgent := analysis.UrgentItems[0]
		rec := Recommendation{
			Type:       "priority",
			Message:    fmt.Sprintf("Focus on '%s' first - urgency score: %d (%s)", topUrgent.Title, topUrgent.UrgencyScore, topUrgent.Reason),
			TaskIDs:    []string{topUrgent.TaskID},
			Confidence: 0.92,
		}
		recommendations = append(recommendations, rec)
	}

	if analysis.RequestingUser != nil && (analysis.RequestingUser.Status == "overloaded" || analysis.RequestingUser.Status == "severely_overloaded") {
		rec := Recommendation{
			Type:       "workload",
			Message:    fmt.Sprintf("Your workload is %s (%s utilization) - consider delegating or deferring lower priority tasks", analysis.RequestingUser.Status, analysis.RequestingUser.CapacityUtilization),
			Confidence: 0.85,
		}
		recommendations = append(recommendations, rec)
	}

	if len(analysis.TeamWorkloads) > 1 {
		overloaded := []UserWorkload{}
		underutilized := []UserWorkload{}

		for _, workload := range analysis.TeamWorkloads {
			if workload.Status == "overloaded" || workload.Status == "severely_overloaded" {
				overloaded = append(overloaded, workload)
			} else if workload.Status == "underutilized" || workload.Status == "normal" {
				underutilized = append(underutilized, workload)
			}
		}

		if len(overloaded) > 0 && len(underutilized) > 0 {
			rec := Recommendation{
				Type:              "delegation",
				Message:           fmt.Sprintf("Consider redistributing tasks from %s (%s) to %s (%s)", overloaded[0].Name, overloaded[0].CapacityUtilization, underutilized[0].Name, underutilized[0].CapacityUtilization),
				SuggestedAssignee: underutilized[0].UserID,
				Confidence:        0.78,
			}
			recommendations = append(recommendations, rec)
		}
	}

	for _, bottleneck := range analysis.Bottlenecks {
		if bottleneck.StuckTasks >= 3 {
			rec := Recommendation{
				Type:          "process",
				Message:       fmt.Sprintf("'%s' column in %s has bottleneck - %d tasks waiting %.1f days on average", bottleneck.Column, bottleneck.Project, bottleneck.StuckTasks, bottleneck.AvgWaitTimeDays),
				AffectedTasks: bottleneck.TaskIDs,
				Confidence:    0.85,
			}
			recommendations = append(recommendations, rec)
		}
	}

	return recommendations
}

func (h *PrioritiesHandler) matchesUserID(assigneeID, targetUserID string) bool {

	if assigneeID == targetUserID {
		return true
	}

	assigneeInt, err1 := strconv.Atoi(assigneeID)
	targetInt, err2 := strconv.Atoi(targetUserID)

	if err1 == nil && err2 == nil {
		return assigneeInt == targetInt
	}

	if err1 == nil {
		return strconv.Itoa(assigneeInt) == targetUserID
	}

	if err2 == nil {
		return assigneeID == strconv.Itoa(targetInt)
	}

	return false
}
