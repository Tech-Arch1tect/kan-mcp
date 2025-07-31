package handlers

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tech-arch1tect/kan-mcp/internal/api"
	"github.com/tech-arch1tect/kan-mcp/internal/auth"
	"github.com/tech-arch1tect/kan-mcp/internal/models"
)

type OverviewHandler struct {
	authManager *auth.AuthManager
	config      *models.UserConfig
}

func NewOverviewHandler(authManager *auth.AuthManager, config *models.UserConfig) *OverviewHandler {
	return &OverviewHandler{
		authManager: authManager,
		config:      config,
	}
}

type OverviewRequest struct {
	IncludeTaskCounts       bool `json:"include_task_counts"`
	IncludeInactiveProjects bool `json:"include_inactive_projects"`
}

type ProjectOverview struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	IsActive    bool           `json:"is_active"`
	Owner       string         `json:"owner"`
	Columns     []ColumnInfo   `json:"columns"`
	Swimlanes   []SwimlaneInfo `json:"swimlanes"`
	TaskCounts  map[string]int `json:"task_counts,omitempty"`
	Users       []ProjectUser  `json:"users"`
}

type ColumnInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Position  int    `json:"position"`
	TaskLimit int    `json:"task_limit"`
}

type SwimlaneInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	IsActive bool   `json:"is_active"`
}

type ProjectUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

type OverviewResponse struct {
	Summary  OverviewSummary   `json:"summary"`
	Projects []ProjectOverview `json:"projects"`
	UserInfo UserInfo          `json:"user_info"`
}

type OverviewSummary struct {
	TotalProjects    int `json:"total_projects"`
	ActiveProjects   int `json:"active_projects"`
	InactiveProjects int `json:"inactive_projects"`
	TotalTasks       int `json:"total_tasks,omitempty"`
}

type UserInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

func (h *OverviewHandler) Handle(params map[string]interface{}, userID string) (*models.MCPResponse, error) {

	var req OverviewRequest
	req.IncludeTaskCounts = true
	req.IncludeInactiveProjects = false

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, fmt.Errorf("failed to parse overview request: %w", err)
		}
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

	userInfo, err := h.getUserInfo(client)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	projectsRaw, err := client.GetMyProjectsRaw()
	if err != nil {
		return nil, fmt.Errorf("failed to get projects: %w", err)
	}

	var rawProjects []map[string]interface{}
	if err := json.Unmarshal(projectsRaw, &rawProjects); err != nil {
		return nil, fmt.Errorf("failed to parse projects: %w", err)
	}

	projectOverviews, err := h.buildProjectOverviews(client, rawProjects, req)
	if err != nil {
		return nil, fmt.Errorf("failed to build project overviews: %w", err)
	}

	if !req.IncludeInactiveProjects {
		filtered := make([]ProjectOverview, 0, len(projectOverviews))
		for _, project := range projectOverviews {
			if project.IsActive {
				filtered = append(filtered, project)
			}
		}
		projectOverviews = filtered
	}

	summary := h.calculateSummary(projectOverviews, req.IncludeTaskCounts)

	response := OverviewResponse{
		Summary:  summary,
		Projects: projectOverviews,
		UserInfo: *userInfo,
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal overview response: %w", err)
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

func (h *OverviewHandler) getUserInfo(client *api.Client) (*UserInfo, error) {
	userRaw, err := client.GetMe()
	if err != nil {
		return nil, err
	}

	return &UserInfo{
		ID:       fmt.Sprintf("%d", userRaw.ID),
		Username: userRaw.Username,
		Name:     userRaw.Name,
	}, nil
}

func (h *OverviewHandler) buildProjectOverviews(client *api.Client, rawProjects []map[string]interface{}, req OverviewRequest) ([]ProjectOverview, error) {
	projectOverviews := make([]ProjectOverview, len(rawProjects))
	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := make([]error, 0)

	for i, rawProject := range rawProjects {
		wg.Add(1)
		go func(index int, project map[string]interface{}) {
			defer wg.Done()

			overview, err := h.buildSingleProjectOverview(client, project, req)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("project %v: %w", project["id"], err))
				mu.Unlock()
				return
			}

			mu.Lock()
			projectOverviews[index] = *overview
			mu.Unlock()
		}(i, rawProject)
	}

	wg.Wait()

	if len(errors) > 0 {
		return nil, fmt.Errorf("failed to build some project overviews: %v", errors[0])
	}

	return projectOverviews, nil
}

func (h *OverviewHandler) buildSingleProjectOverview(client *api.Client, rawProject map[string]interface{}, req OverviewRequest) (*ProjectOverview, error) {
	projectID := fmt.Sprintf("%.0f", rawProject["id"].(float64))
	projectIDInt := int(rawProject["id"].(float64))

	columns, err := h.getProjectColumns(client, projectIDInt)
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	swimlanes, err := h.getProjectSwimlanes(client, projectIDInt)
	if err != nil {
		return nil, fmt.Errorf("failed to get swimlanes: %w", err)
	}

	users, err := h.getProjectUsers(client, projectIDInt)
	if err != nil {
		return nil, fmt.Errorf("failed to get users: %w", err)
	}

	overview := &ProjectOverview{
		ID:          projectID,
		Name:        h.getString(rawProject, "name"),
		Description: h.getString(rawProject, "description"),
		IsActive:    h.getBool(rawProject, "is_active"),
		Owner:       h.getString(rawProject, "owner_name"),
		Columns:     columns,
		Swimlanes:   swimlanes,
		Users:       users,
	}

	if req.IncludeTaskCounts {
		taskCounts, err := h.getProjectTaskCounts(client, projectIDInt, columns)
		if err != nil {
			return nil, fmt.Errorf("failed to get task counts: %w", err)
		}
		overview.TaskCounts = taskCounts
	}

	return overview, nil
}

func (h *OverviewHandler) getProjectColumns(client *api.Client, projectID int) ([]ColumnInfo, error) {
	columns, err := client.GetColumns(projectID)
	if err != nil {
		return nil, err
	}

	result := make([]ColumnInfo, len(columns))
	for i, col := range columns {
		result[i] = ColumnInfo{
			ID:        fmt.Sprintf("%d", col.ID),
			Title:     col.Title,
			Position:  col.Position,
			TaskLimit: col.TaskLimit,
		}
	}

	return result, nil
}

func (h *OverviewHandler) getProjectSwimlanes(client *api.Client, projectID int) ([]SwimlaneInfo, error) {
	swimlanes, err := client.GetSwimlanes(projectID)
	if err != nil {
		return nil, err
	}

	result := make([]SwimlaneInfo, len(swimlanes))
	for i, lane := range swimlanes {
		result[i] = SwimlaneInfo{
			ID:       fmt.Sprintf("%d", lane.ID),
			Name:     lane.Name,
			Position: lane.Position,
			IsActive: bool(lane.IsActive),
		}
	}

	return result, nil
}

func (h *OverviewHandler) getProjectUsers(client *api.Client, projectID int) ([]ProjectUser, error) {
	users, err := client.GetProjectUsers(projectID)
	if err != nil {
		return nil, err
	}

	result := make([]ProjectUser, len(users))
	for i, user := range users {
		result[i] = ProjectUser{
			ID:       fmt.Sprintf("%d", user.ID),
			Username: user.Username,
			Name:     user.Name,
			Role:     user.Role,
		}
	}

	return result, nil
}

func (h *OverviewHandler) getProjectTaskCounts(client *api.Client, projectID int, columns []ColumnInfo) (map[string]int, error) {

	tasks, err := client.GetTasksByProject(projectID)
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int)

	for _, col := range columns {
		counts[col.Title] = 0
	}

	for _, task := range tasks {
		columnID := fmt.Sprintf("%d", task.ColumnID)

		for _, col := range columns {
			if col.ID == columnID {
				counts[col.Title]++
				break
			}
		}
	}

	return counts, nil
}

func (h *OverviewHandler) calculateSummary(projects []ProjectOverview, includeTaskCounts bool) OverviewSummary {
	summary := OverviewSummary{
		TotalProjects: len(projects),
	}

	for _, project := range projects {
		if project.IsActive {
			summary.ActiveProjects++
		} else {
			summary.InactiveProjects++
		}

		if includeTaskCounts {
			for _, count := range project.TaskCounts {
				summary.TotalTasks += count
			}
		}
	}

	return summary
}

func (h *OverviewHandler) getString(data map[string]interface{}, key string) string {
	if val, ok := data[key]; ok && val != nil {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func (h *OverviewHandler) getBool(data map[string]interface{}, key string) bool {
	if val, ok := data[key]; ok && val != nil {
		switch v := val.(type) {
		case bool:
			return v
		case string:
			return v == "1" || v == "true"
		case float64:
			return v == 1
		}
	}
	return false
}
