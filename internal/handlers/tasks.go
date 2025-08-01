package handlers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tech-arch1tect/kan-mcp/internal/api"
	"github.com/tech-arch1tect/kan-mcp/internal/auth"
	"github.com/tech-arch1tect/kan-mcp/internal/models"
)

const (
	MaxResponseSize     = 200 * 1024
	WarningResponseSize = 150 * 1024
	MaxTasksHardLimit   = 100
)

type TasksHandler struct {
	authManager *auth.AuthManager
	config      *models.UserConfig
}

func NewTasksHandler(authManager *auth.AuthManager, config *models.UserConfig) *TasksHandler {
	return &TasksHandler{
		authManager: authManager,
		config:      config,
	}
}

type TasksRequest struct {
	ProjectIDs          []string   `json:"project_ids"`
	AssigneeIDs         []string   `json:"assignee_ids"`
	StatusFilter        string     `json:"status_filter"`
	DueDateRange        *DateRange `json:"due_date_range"`
	IncludeOverdue      bool       `json:"include_overdue"`
	IncludeTimeTracking bool       `json:"include_time_tracking"`
	SortBy              string     `json:"sort_by"`
	Limit               int        `json:"limit"`
	SummaryMode         bool       `json:"summary_mode"`
}

type DateRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type TaskDetail struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Project      ProjectInfo   `json:"project"`
	Assignee     *UserInfo     `json:"assignee"`
	Status       TaskStatus    `json:"status"`
	Dates        TaskDates     `json:"dates"`
	TimeTracking *TimeTracking `json:"time_tracking,omitempty"`
	Priority     string        `json:"priority"`
	Category     string        `json:"category"`
	Tags         []string      `json:"tags"`
	URL          string        `json:"url"`
	IsOverdue    bool          `json:"is_overdue"`
	DaysUntilDue *int          `json:"days_until_due"`
}

type TaskSummary struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	Project      ProjectInfo `json:"project"`
	Assignee     *UserInfo   `json:"assignee,omitempty"`
	Status       string      `json:"status"`
	DueDate      string      `json:"due_date,omitempty"`
	IsOverdue    bool        `json:"is_overdue"`
	DaysUntilDue *int        `json:"days_until_due,omitempty"`
}

type ProjectInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TaskStatus struct {
	Column   string `json:"column"`
	Swimlane string `json:"swimlane"`
}

type TaskDates struct {
	Created  string `json:"created"`
	Due      string `json:"due"`
	Modified string `json:"modified"`
	Started  string `json:"started"`
}

type TimeTracking struct {
	EstimatedHours float64 `json:"estimated_hours"`
	SpentHours     float64 `json:"spent_hours"`
	RemainingHours float64 `json:"remaining_hours"`
}

type TasksSummary struct {
	TotalTasks      int `json:"total_tasks"`
	OverdueTasks    int `json:"overdue_tasks"`
	DueThisWeek     int `json:"due_this_week"`
	UnassignedTasks int `json:"unassigned_tasks"`
}

type TasksResponse struct {
	Summary       TasksSummary  `json:"summary"`
	Tasks         []TaskDetail  `json:"tasks,omitempty"`
	TaskSummaries []TaskSummary `json:"task_summaries,omitempty"`
	Truncated     bool          `json:"truncated,omitempty"`
	TruncatedAt   int           `json:"truncated_at,omitempty"`
	ResponseSize  int           `json:"response_size_bytes,omitempty"`
}

func (h *TasksHandler) Handle(params map[string]interface{}, userID string) (*models.MCPResponse, error) {
	var req TasksRequest
	req.StatusFilter = "active"
	req.IncludeOverdue = false
	req.IncludeTimeTracking = true
	req.SortBy = "due_date"
	req.Limit = 20
	req.SummaryMode = true

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, fmt.Errorf("failed to parse tasks request: %w", err)
		}
	}

	if req.Limit > MaxTasksHardLimit {
		req.Limit = MaxTasksHardLimit
	}

	if req.SummaryMode && req.Limit > MaxTasksHardLimit*2 {
		req.Limit = MaxTasksHardLimit * 2
	}

	user, err := h.authManager.AuthenticateUser(userID)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	token, err := h.authManager.GetDecryptedToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt token: %w", err)
	}

	kanboardURL := user.KanboardURL
	if kanboardURL == "" {
		kanboardURL = h.config.DefaultKanboardURL
	}

	client := api.NewClient(kanboardURL, user.KanboardUsername, token)

	projects, err := h.getFilteredProjects(client, req.ProjectIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get projects: %w", err)
	}

	tasks, err := h.collectTasks(client, projects, kanboardURL, req.IncludeTimeTracking)
	if err != nil {
		return nil, fmt.Errorf("failed to collect tasks: %w", err)
	}

	filteredTasks := h.filterTasks(tasks, req)
	sortedTasks := h.sortTasks(filteredTasks, req.SortBy)

	summary := h.calculateTasksSummary(sortedTasks)

	var response TasksResponse
	var responseJSON []byte

	if req.SummaryMode {

		taskSummaries := h.createTaskSummaries(sortedTasks, req.Limit)
		response = TasksResponse{
			Summary:       summary,
			TaskSummaries: taskSummaries,
		}
	} else {

		finalTasks, truncated, truncatedAt := h.applyResponseSizeLimits(sortedTasks, req.Limit)
		response = TasksResponse{
			Summary:     summary,
			Tasks:       finalTasks,
			Truncated:   truncated,
			TruncatedAt: truncatedAt,
		}
	}

	responseJSON, err = json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tasks response: %w", err)
	}

	response.ResponseSize = len(responseJSON)
	if response.ResponseSize > WarningResponseSize {
		responseJSON, _ = json.MarshalIndent(response, "", "  ")
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

type ProjectData struct {
	ID   int
	Name string
}

func (h *TasksHandler) getFilteredProjects(client *api.Client, projectIDs []string) ([]ProjectData, error) {
	projectsRaw, err := client.GetMyProjectsRaw()
	if err != nil {
		return nil, err
	}

	var rawProjects []map[string]interface{}
	if err := json.Unmarshal(projectsRaw, &rawProjects); err != nil {
		return nil, err
	}

	var projects []ProjectData
	for _, rawProject := range rawProjects {
		projectID := fmt.Sprintf("%.0f", rawProject["id"].(float64))

		if len(projectIDs) > 0 {
			found := false
			for _, filterID := range projectIDs {
				if projectID == filterID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		project := ProjectData{
			ID:   int(rawProject["id"].(float64)),
			Name: h.getString(rawProject, "name"),
		}
		projects = append(projects, project)
	}

	return projects, nil
}

func (h *TasksHandler) collectTasks(client *api.Client, projects []ProjectData, baseURL string, includeTimeTracking bool) ([]TaskDetail, error) {
	var allTasks []TaskDetail
	var mu sync.Mutex
	var wg sync.WaitGroup
	errors := make([]error, 0)

	for _, project := range projects {
		wg.Add(1)
		go func(proj ProjectData) {
			defer wg.Done()

			projectTasks, err := h.getProjectTasks(client, proj, baseURL, includeTimeTracking)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("project %d: %w", proj.ID, err))
				mu.Unlock()
				return
			}

			mu.Lock()
			allTasks = append(allTasks, projectTasks...)
			mu.Unlock()
		}(project)
	}

	wg.Wait()

	if len(errors) > 0 {
		return nil, errors[0]
	}

	return allTasks, nil
}

func (h *TasksHandler) getProjectTasks(client *api.Client, project ProjectData, baseURL string, includeTimeTracking bool) ([]TaskDetail, error) {
	tasks, err := client.GetTasksByProject(project.ID)
	if err != nil {
		return nil, err
	}

	columns, err := client.GetColumns(project.ID)
	if err != nil {
		return nil, err
	}

	columnMap := make(map[int]string)
	for _, col := range columns {
		columnMap[col.ID] = col.Title
	}

	swimlanes, err := client.GetSwimlanes(project.ID)
	if err != nil {
		return nil, err
	}

	swimlaneMap := make(map[int]string)
	for _, lane := range swimlanes {
		swimlaneMap[lane.ID] = lane.Name
	}

	users, err := client.GetProjectUsers(project.ID)
	if err != nil {
		return nil, err
	}

	userMap := make(map[int]*UserInfo)
	for _, user := range users {
		userMap[user.ID] = &UserInfo{
			ID:       fmt.Sprintf("%d", user.ID),
			Username: user.Username,
			Name:     user.Name,
		}
	}

	var taskDetails []TaskDetail
	for _, task := range tasks {
		detail := h.buildTaskDetail(task, project, columnMap, swimlaneMap, userMap, baseURL, includeTimeTracking)
		taskDetails = append(taskDetails, detail)
	}

	return taskDetails, nil
}

func (h *TasksHandler) buildTaskDetail(task models.Task, project ProjectData, columnMap map[int]string, swimlaneMap map[int]string, userMap map[int]*UserInfo, baseURL string, includeTimeTracking bool) TaskDetail {
	detail := TaskDetail{
		ID:          fmt.Sprintf("%d", task.ID),
		Title:       task.Title,
		Description: task.Description,
		Project: ProjectInfo{
			ID:   fmt.Sprintf("%d", project.ID),
			Name: project.Name,
		},
		Status: TaskStatus{
			Column:   columnMap[task.ColumnID],
			Swimlane: swimlaneMap[task.SwimlaneID],
		},
		Priority: "normal",
		Category: "",
		URL:      fmt.Sprintf("%s/?controller=TaskViewController&action=show&task_id=%d&project_id=%d", baseURL, task.ID, project.ID),
	}

	if task.OwnerID > 0 {
		if user, exists := userMap[task.OwnerID]; exists {
			detail.Assignee = user
		}
	}

	detail.Dates = TaskDates{
		Created:  h.formatKanboardTime(task.DateCreation),
		Due:      h.formatKanboardTime(task.DateDue),
		Modified: h.formatKanboardTime(task.DateModified),
		Started:  h.formatKanboardTime(task.DateStarted),
	}

	if !task.DateDue.Time.IsZero() {
		detail.IsOverdue, detail.DaysUntilDue = h.calculateDueDateInfo(task.DateDue.Time.Format("2006-01-02T15:04:05Z"))
	}

	if includeTimeTracking {
		detail.TimeTracking = &TimeTracking{
			EstimatedHours: task.TimeEstimated,
			SpentHours:     task.TimeSpent,
			RemainingHours: task.TimeEstimated - task.TimeSpent,
		}
	}

	return detail
}

func (h *TasksHandler) filterTasks(tasks []TaskDetail, req TasksRequest) []TaskDetail {
	filtered := make([]TaskDetail, 0, len(tasks))

	for _, task := range tasks {
		if !h.shouldIncludeTask(task, req) {
			continue
		}
		filtered = append(filtered, task)
	}

	return filtered
}

func (h *TasksHandler) shouldIncludeTask(task TaskDetail, req TasksRequest) bool {
	if req.StatusFilter == "active" && h.isTaskCompleted(task) {
		return false
	}
	if req.StatusFilter == "completed" && !h.isTaskCompleted(task) {
		return false
	}

	if len(req.AssigneeIDs) > 0 {
		if task.Assignee == nil {
			return false
		}
		found := false
		for _, assigneeID := range req.AssigneeIDs {
			if task.Assignee.ID == assigneeID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if !req.IncludeOverdue && task.IsOverdue {
		return false
	}

	if req.DueDateRange != nil {
		if !h.isTaskInDateRange(task, req.DueDateRange) {
			return false
		}
	}

	return true
}

func (h *TasksHandler) isTaskCompleted(task TaskDetail) bool {
	completedColumns := []string{"Done", "Completed", "Closed", "Finished"}
	for _, col := range completedColumns {
		if strings.EqualFold(task.Status.Column, col) {
			return true
		}
	}
	return false
}

func (h *TasksHandler) isTaskInDateRange(task TaskDetail, dateRange *DateRange) bool {
	if task.Dates.Due == "" {
		return false
	}

	dueDate, err := time.Parse("2006-01-02T15:04:05Z", task.Dates.Due)
	if err != nil {
		return false
	}

	if dateRange.Start != "" {
		startDate, err := time.Parse("2006-01-02", dateRange.Start)
		if err != nil {
			return false
		}
		if dueDate.Before(startDate) {
			return false
		}
	}

	if dateRange.End != "" {
		endDate, err := time.Parse("2006-01-02", dateRange.End)
		if err != nil {
			return false
		}
		if dueDate.After(endDate) {
			return false
		}
	}

	return true
}

func (h *TasksHandler) sortTasks(tasks []TaskDetail, sortBy string) []TaskDetail {
	sorted := make([]TaskDetail, len(tasks))
	copy(sorted, tasks)

	switch sortBy {
	case "due_date":
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Dates.Due == "" && sorted[j].Dates.Due == "" {
				return false
			}
			if sorted[i].Dates.Due == "" {
				return false
			}
			if sorted[j].Dates.Due == "" {
				return true
			}
			return sorted[i].Dates.Due < sorted[j].Dates.Due
		})
	case "priority":
		sort.Slice(sorted, func(i, j int) bool {
			return h.getPriorityValue(sorted[i].Priority) > h.getPriorityValue(sorted[j].Priority)
		})
	case "created":
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Dates.Created > sorted[j].Dates.Created
		})
	default:
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Dates.Due == "" && sorted[j].Dates.Due == "" {
				return false
			}
			if sorted[i].Dates.Due == "" {
				return false
			}
			if sorted[j].Dates.Due == "" {
				return true
			}
			return sorted[i].Dates.Due < sorted[j].Dates.Due
		})
	}

	return sorted
}

func (h *TasksHandler) calculateTasksSummary(tasks []TaskDetail) TasksSummary {
	summary := TasksSummary{
		TotalTasks: len(tasks),
	}

	now := time.Now()
	weekFromNow := now.AddDate(0, 0, 7)

	for _, task := range tasks {
		if task.IsOverdue {
			summary.OverdueTasks++
		}

		if task.Assignee == nil {
			summary.UnassignedTasks++
		}

		if task.Dates.Due != "" {
			dueDate, err := time.Parse("2006-01-02T15:04:05Z", task.Dates.Due)
			if err == nil && dueDate.Before(weekFromNow) && dueDate.After(now) {
				summary.DueThisWeek++
			}
		}
	}

	return summary
}

func (h *TasksHandler) calculateDueDateInfo(dueDateStr string) (bool, *int) {
	if dueDateStr == "" {
		return false, nil
	}

	dueDate, err := time.Parse("2006-01-02T15:04:05Z", dueDateStr)
	if err != nil {
		dueDate, err = time.Parse("2006-01-02", dueDateStr)
		if err != nil {
			return false, nil
		}
	}

	now := time.Now()
	days := int(dueDate.Sub(now).Hours() / 24)

	isOverdue := dueDate.Before(now)

	return isOverdue, &days
}

func (h *TasksHandler) formatKanboardTime(kt models.KanboardTime) string {
	if kt.Time.IsZero() {
		return ""
	}
	return kt.Time.Format("2006-01-02T15:04:05Z")
}

func (h *TasksHandler) formatDate(timestamp interface{}) string {
	if timestamp == nil {
		return ""
	}

	switch v := timestamp.(type) {
	case string:
		if v == "" || v == "0" {
			return ""
		}
		ts, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return v
		}
		return time.Unix(ts, 0).UTC().Format("2006-01-02T15:04:05Z")
	case float64:
		if v == 0 {
			return ""
		}
		return time.Unix(int64(v), 0).UTC().Format("2006-01-02T15:04:05Z")
	case int64:
		if v == 0 {
			return ""
		}
		return time.Unix(v, 0).UTC().Format("2006-01-02T15:04:05Z")
	default:
		return ""
	}
}

func (h *TasksHandler) getPriorityString(priority interface{}) string {
	switch v := priority.(type) {
	case float64:
		switch int(v) {
		case 0:
			return "low"
		case 1:
			return "normal"
		case 2:
			return "high"
		case 3:
			return "urgent"
		default:
			return "normal"
		}
	case string:
		return v
	default:
		return "normal"
	}
}

func (h *TasksHandler) getPriorityValue(priority string) int {
	switch priority {
	case "urgent":
		return 3
	case "high":
		return 2
	case "normal":
		return 1
	case "low":
		return 0
	default:
		return 1
	}
}

func (h *TasksHandler) createTaskSummaries(tasks []TaskDetail, limit int) []TaskSummary {
	if len(tasks) > limit {
		tasks = tasks[:limit]
	}

	summaries := make([]TaskSummary, len(tasks))
	for i, task := range tasks {
		var assignee *UserInfo
		if task.Assignee != nil {
			assignee = &UserInfo{
				ID:       task.Assignee.ID,
				Username: task.Assignee.Username,
				Name:     task.Assignee.Name,
			}
		}

		summaries[i] = TaskSummary{
			ID:           task.ID,
			Title:        task.Title,
			Project:      task.Project,
			Assignee:     assignee,
			Status:       task.Status.Column,
			DueDate:      task.Dates.Due,
			IsOverdue:    task.IsOverdue,
			DaysUntilDue: task.DaysUntilDue,
		}
	}

	return summaries
}

func (h *TasksHandler) applyResponseSizeLimits(tasks []TaskDetail, requestedLimit int) ([]TaskDetail, bool, int) {
	if len(tasks) > requestedLimit {
		tasks = tasks[:requestedLimit]
	}

	for limit := len(tasks); limit > 0; limit-- {
		testTasks := tasks[:limit]
		testResponse := TasksResponse{
			Summary: TasksSummary{},
			Tasks:   testTasks,
		}

		testJSON, err := json.Marshal(testResponse)
		if err != nil {
			continue
		}

		if len(testJSON) <= MaxResponseSize {
			if limit < len(tasks) {
				return testTasks, true, limit
			}
			return testTasks, false, 0
		}
	}

	return []TaskDetail{}, true, 0
}

func (h *TasksHandler) getString(data map[string]interface{}, key string) string {
	if val, ok := data[key]; ok && val != nil {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}
