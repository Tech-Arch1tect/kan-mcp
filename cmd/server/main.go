package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"syscall"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tech-arch1tect/kan-mcp/internal/auth"
	"github.com/tech-arch1tect/kan-mcp/internal/config"
	"github.com/tech-arch1tect/kan-mcp/internal/handlers"
	"github.com/tech-arch1tect/kan-mcp/internal/models"
	"github.com/tech-arch1tect/kan-mcp/internal/storage"
	"golang.org/x/term"
)

type userIDKey struct{}

func withUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

func userIDFromContext(ctx context.Context) (string, error) {
	userID, ok := ctx.Value(userIDKey{}).(string)
	if !ok || userID == "" {
		return "", fmt.Errorf("missing user ID")
	}
	return userID, nil
}

type KanboardMCPServer struct {
	server      *server.MCPServer
	authManager *auth.AuthManager
	userConfig  *models.UserConfig
}

func NewKanboardMCPServer() (*KanboardMCPServer, error) {

	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	encryptionKey, err := cfg.GetEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get encryption key: %w", err)
	}

	fileStore, err := storage.NewFileStore(cfg.Storage.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize file store: %w", err)
	}

	authManager, err := auth.NewAuthManager(encryptionKey, fileStore)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize auth manager: %w", err)
	}

	userConfig := &models.UserConfig{
		DefaultKanboardURL: cfg.Kanboard.DefaultURL,
		EncryptionKey:      encryptionKey,
	}

	mcpServer := server.NewMCPServer(
		"Kanboard MCP Server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	kanboardServer := &KanboardMCPServer{
		server:      mcpServer,
		authManager: authManager,
		userConfig:  userConfig,
	}

	kanboardServer.addTools()

	return kanboardServer, nil
}

func (s *KanboardMCPServer) addTools() {

	overviewTool := mcp.NewTool("kanboard_overview",
		mcp.WithDescription("Get complete overview of all accessible projects and their board structures"),
		mcp.WithString("user_id",
			mcp.Description("User ID for authentication"),
			mcp.Required(),
		),
		mcp.WithBoolean("include_task_counts",
			mcp.Description("Include task counts per column (default: true)"),
		),
		mcp.WithBoolean("include_inactive_projects",
			mcp.Description("Include inactive/archived projects (default: false)"),
		),
	)
	s.server.AddTool(overviewTool, s.handleOverview)

	tasksTool := mcp.NewTool("kanboard_tasks",
		mcp.WithDescription("Get detailed task information for priority analysis and workload management"),
		mcp.WithString("user_id",
			mcp.Description("User ID for authentication"),
			mcp.Required(),
		),
		mcp.WithString("project_ids",
			mcp.Description("Optional: comma-separated list of project IDs to filter by"),
		),
		mcp.WithString("assignee_ids",
			mcp.Description("Optional: comma-separated list of assignee user IDs to filter by"),
		),
		mcp.WithString("status_filter",
			mcp.Description("Filter tasks by status: 'active', 'completed', or 'all' (default: active)"),
		),
		mcp.WithString("due_date_start",
			mcp.Description("Optional: filter by due date start (YYYY-MM-DD format)"),
		),
		mcp.WithString("due_date_end",
			mcp.Description("Optional: filter by due date end (YYYY-MM-DD format)"),
		),
		mcp.WithBoolean("include_overdue",
			mcp.Description("Include overdue tasks (default: false)"),
		),
		mcp.WithBoolean("include_time_tracking",
			mcp.Description("Include time tracking information (default: true)"),
		),
		mcp.WithString("sort_by",
			mcp.Description("Sort tasks by: 'due_date', 'priority', or 'created' (default: due_date)"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of tasks to return (default: 20, max: 100, or 200 in summary mode)"),
		),
		mcp.WithBoolean("summary_mode",
			mcp.Description("Return lightweight task summaries instead of full details (default: true)"),
		),
	)
	s.server.AddTool(tasksTool, s.handleTasks)

	prioritiesTool := mcp.NewTool("kanboard_priorities",
		mcp.WithDescription("Analyse workload and provide priority recommendations"),
		mcp.WithString("user_id",
			mcp.Description("User ID for authentication"),
			mcp.Required(),
		),
		mcp.WithString("project_ids",
			mcp.Description("Optional: comma-separated list of project IDs to filter by"),
		),
		mcp.WithString("time_horizon",
			mcp.Description("Time horizon for analysis: 'today', 'week', or 'month' (default: week)"),
		),
		mcp.WithBoolean("include_recommendations",
			mcp.Description("Include priority recommendations (default: true)"),
		),
	)
	s.server.AddTool(prioritiesTool, s.handlePriorities)

	analyticsTool := mcp.NewTool("kanboard_analytics",
		mcp.WithDescription("Perform historical data analysis and trend identification"),
		mcp.WithString("user_id",
			mcp.Description("User ID for authentication"),
			mcp.Required(),
		),
		mcp.WithString("project_ids",
			mcp.Description("Optional: comma-separated list of project IDs to filter by"),
		),
		mcp.WithString("time_range",
			mcp.Description("Time range for analysis: '7_days', '30_days', '90_days', '6_months', '1_year' (default: 30_days)"),
		),
		mcp.WithString("analysis_types",
			mcp.Description("Comma-separated analysis types: 'completion_trends', 'cycle_time', 'velocity', 'task_aging', 'burndown', 'project_health' (default: all)"),
		),
		mcp.WithString("group_by",
			mcp.Description("Group results by: 'project', 'user', 'time' (default: project)"),
		),
	)
	s.server.AddTool(analyticsTool, s.handleAnalytics)
}

func (s *KanboardMCPServer) handleOverview(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {

	args := request.GetArguments()

	userID, ok := args["user_id"].(string)
	if !ok || userID == "" {
		return mcp.NewToolResultError("Missing required parameter: user_id. Please ask the user for their User ID and include it in the tool call. Users can find their User ID by running: ./kan-mcp cli list"), nil
	}

	params := make(map[string]interface{})

	if val, ok := args["include_task_counts"]; ok {
		params["include_task_counts"] = val
	}

	if val, ok := args["include_inactive_projects"]; ok {
		params["include_inactive_projects"] = val
	}

	overviewHandler := handlers.NewOverviewHandler(s.authManager, s.userConfig)

	response, err := overviewHandler.Handle(params, userID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("overview failed: %v", err)), nil
	}

	if len(response.Content) > 0 {
		return mcp.NewToolResultText(response.Content[0].Text), nil
	}

	return mcp.NewToolResultText("{}"), nil
}

func (s *KanboardMCPServer) handleTasks(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {

	args := request.GetArguments()

	userID, ok := args["user_id"].(string)
	if !ok || userID == "" {
		return mcp.NewToolResultError("Missing required parameter: user_id. Please ask the user for their User ID and include it in the tool call. Users can find their User ID by running: ./kan-mcp cli list"), nil
	}

	params := make(map[string]interface{})

	if val, ok := args["project_ids"]; ok {
		if str, ok := val.(string); ok && str != "" {
			params["project_ids"] = strings.Split(str, ",")
		}
	}

	if val, ok := args["assignee_ids"]; ok {
		if str, ok := val.(string); ok && str != "" {
			params["assignee_ids"] = strings.Split(str, ",")
		}
	}

	if val, ok := args["status_filter"]; ok {
		params["status_filter"] = val
	}

	if startVal, ok := args["due_date_start"]; ok {
		if endVal, ok := args["due_date_end"]; ok {
			params["due_date_range"] = map[string]interface{}{
				"start": startVal,
				"end":   endVal,
			}
		} else if startVal != nil {
			params["due_date_range"] = map[string]interface{}{
				"start": startVal,
			}
		}
	} else if endVal, ok := args["due_date_end"]; ok {
		params["due_date_range"] = map[string]interface{}{
			"end": endVal,
		}
	}

	if val, ok := args["include_overdue"]; ok {
		params["include_overdue"] = val
	}

	if val, ok := args["include_time_tracking"]; ok {
		params["include_time_tracking"] = val
	}

	if val, ok := args["sort_by"]; ok {
		params["sort_by"] = val
	}

	if val, ok := args["limit"]; ok {
		params["limit"] = val
	}

	if val, ok := args["summary_mode"]; ok {
		params["summary_mode"] = val
	}

	tasksHandler := handlers.NewTasksHandler(s.authManager, s.userConfig)

	response, err := tasksHandler.Handle(params, userID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("tasks failed: %v", err)), nil
	}

	if len(response.Content) > 0 {
		return mcp.NewToolResultText(response.Content[0].Text), nil
	}

	return mcp.NewToolResultText("{}"), nil
}

func (s *KanboardMCPServer) handlePriorities(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {

	args := request.GetArguments()

	userID, ok := args["user_id"].(string)
	if !ok || userID == "" {
		return mcp.NewToolResultError("Missing required parameter: user_id. Please ask the user for their User ID and include it in the tool call. Users can find their User ID by running: ./kan-mcp cli list"), nil
	}

	params := make(map[string]interface{})

	if val, ok := args["project_ids"]; ok {
		if str, ok := val.(string); ok && str != "" {
			params["project_ids"] = strings.Split(str, ",")
		}
	}

	if val, ok := args["time_horizon"]; ok {
		params["time_horizon"] = val
	}

	if val, ok := args["include_recommendations"]; ok {
		params["include_recommendations"] = val
	}

	prioritiesHandler := handlers.NewPrioritiesHandler(s.authManager, s.userConfig)

	response, err := prioritiesHandler.Handle(params, userID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("priorities failed: %v", err)), nil
	}

	if len(response.Content) > 0 {
		return mcp.NewToolResultText(response.Content[0].Text), nil
	}

	return mcp.NewToolResultText("{}"), nil
}

func (s *KanboardMCPServer) handleAnalytics(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {

	args := request.GetArguments()

	userID, ok := args["user_id"].(string)
	if !ok || userID == "" {
		return mcp.NewToolResultError("Missing required parameter: user_id. Please ask the user for their User ID and include it in the tool call. Users can find their User ID by running: ./kan-mcp cli list"), nil
	}

	params := make(map[string]interface{})

	if val, ok := args["project_ids"]; ok {
		if str, ok := val.(string); ok && str != "" {
			params["project_ids"] = strings.Split(str, ",")
		}
	}

	if val, ok := args["time_range"]; ok {
		params["time_range"] = val
	}

	if val, ok := args["analysis_types"]; ok {
		if str, ok := val.(string); ok && str != "" {
			params["analysis_types"] = strings.Split(str, ",")
		}
	}

	if val, ok := args["group_by"]; ok {
		params["group_by"] = val
	}

	analyticsHandler := handlers.NewAnalyticsHandler(s.authManager, s.userConfig)

	response, err := analyticsHandler.Handle(params, userID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("analytics failed: %v", err)), nil
	}

	if len(response.Content) > 0 {
		return mcp.NewToolResultText(response.Content[0].Text), nil
	}

	return mcp.NewToolResultText("{}"), nil
}

func (s *KanboardMCPServer) extractUserIDFromRequest(ctx context.Context, r *http.Request) context.Context {

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		userID = r.URL.Query().Get("user_id")
	}

	log.Printf("Extracted User ID: %s (from header: %s, from query: %s)",
		userID, r.Header.Get("X-User-ID"), r.URL.Query().Get("user_id"))

	if userID != "" {
		return withUserID(ctx, userID)
	}

	return ctx
}

func main() {
	var (
		transport   = flag.String("t", "stdio", "Transport type (stdio or http)")
		cliCommand  = flag.String("cmd", "", "CLI command (register, list, delete, show)")
		userID      = flag.String("user-id", "", "User ID for show/delete operations")
		kanboardURL = flag.String("kanboard-url", "", "Kanboard URL (optional, uses default if not set)")
		username    = flag.String("username", "", "Kanboard username")
	)
	flag.StringVar(transport, "transport", "stdio", "Transport type (stdio or http)")
	flag.Parse()

	if len(os.Args) > 1 && os.Args[1] == "cli" {

		if len(os.Args) > 2 {
			*cliCommand = os.Args[2]

			flag.CommandLine.Parse(os.Args[3:])
		}
		runCLI(*cliCommand, *userID, *kanboardURL, *username)
		return
	}

	log.Println("Starting Kanboard MCP Server...")

	kanboardServer, err := NewKanboardMCPServer()
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	switch *transport {
	case "stdio":
		if err := server.ServeStdio(kanboardServer.server); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case "http":
		httpServer := server.NewStreamableHTTPServer(kanboardServer.server,
			server.WithHTTPContextFunc(kanboardServer.extractUserIDFromRequest),
		)
		log.Printf("HTTP server listening on :8080")
		if err := httpServer.Start(":8080"); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	default:
		log.Fatalf("Invalid transport type: %s. Must be 'stdio' or 'http'", *transport)
	}
}

func runCLI(command, userID, kanboardURL, username string) {

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	encryptionKey, err := cfg.GetEncryptionKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get encryption key: %v\n", err)
		os.Exit(1)
	}

	fileStore, err := storage.NewFileStore(cfg.Storage.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	authManager, err := auth.NewAuthManager(encryptionKey, fileStore)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize auth manager: %v\n", err)
		os.Exit(1)
	}

	switch command {
	case "register":
		if username == "" {
			fmt.Fprintf(os.Stderr, "Username is required for registration\n")
			fmt.Fprintf(os.Stderr, "Usage: %s cli register -username <username> [-kanboard-url <url>]\n", os.Args[0])
			os.Exit(1)
		}
		registerUser(authManager, cfg, kanboardURL, username)
	case "list":
		listUsers(authManager)
	case "delete":
		if userID == "" {
			fmt.Fprintf(os.Stderr, "User ID is required for delete operation\n")
			fmt.Fprintf(os.Stderr, "Usage: %s cli delete -user-id <user-id>\n", os.Args[0])
			os.Exit(1)
		}
		deleteUser(authManager, userID)
	case "show":
		if userID == "" {
			fmt.Fprintf(os.Stderr, "User ID is required for show operation\n")
			fmt.Fprintf(os.Stderr, "Usage: %s cli show -user-id <user-id>\n", os.Args[0])
			os.Exit(1)
		}
		showUser(authManager, userID)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		fmt.Fprintf(os.Stderr, "Available commands: register, list, delete, show\n")
		os.Exit(1)
	}
}

func registerUser(authManager *auth.AuthManager, cfg *config.Config, kanboardURL, username string) {
	fmt.Printf("Registering user: %s\n", username)

	fmt.Print("Enter Kanboard Personal Access Token: ")
	tokenBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nFailed to read token: %v\n", err)
		os.Exit(1)
	}
	token := string(tokenBytes)
	fmt.Println()

	if token == "" {
		fmt.Fprintf(os.Stderr, "Token cannot be empty\n")
		os.Exit(1)
	}

	if kanboardURL == "" {
		kanboardURL = cfg.Kanboard.DefaultURL
	}

	user, err := authManager.RegisterUser(kanboardURL, username, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Registration failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ User registered successfully!\n")
	fmt.Printf("  User ID: %s\n", user.UserID)
	fmt.Printf("  Kanboard URL: %s\n", user.KanboardURL)
	fmt.Printf("  Username: %s\n", user.KanboardUsername)
	fmt.Printf("  Created: %s\n", user.CreatedAt.Format("2006-01-02 15:04:05"))
}

func listUsers(authManager *auth.AuthManager) {
	users, err := authManager.ListUsers()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list users: %v\n", err)
		os.Exit(1)
	}

	if len(users) == 0 {
		fmt.Println("No users registered")
		return
	}

	fmt.Printf("Registered Users (%d):\n", len(users))
	fmt.Println(strings.Repeat("-", 80))

	for _, user := range users {
		fmt.Printf("User ID: %s\n", user.UserID)
		fmt.Printf("Kanboard URL: %s\n", user.KanboardURL)
		fmt.Printf("Username: %s\n", user.KanboardUsername)
		fmt.Printf("Created: %s\n", user.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("Last Used: %s\n", user.LastUsed.Format("2006-01-02 15:04:05"))
		fmt.Println(strings.Repeat("-", 80))
	}
}

func deleteUser(authManager *auth.AuthManager, userID string) {

	fmt.Printf("Are you sure you want to delete user %s? (y/N): ", userID)
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read input: %v\n", err)
		os.Exit(1)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Println("Deletion cancelled")
		return
	}

	if err := authManager.DeleteUser(userID); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to delete user: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ User %s deleted successfully\n", userID)
}

func showUser(authManager *auth.AuthManager, userID string) {
	user, err := authManager.AuthenticateUser(userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "User not found: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("User Details:\n")
	fmt.Printf("  User ID: %s\n", user.UserID)
	fmt.Printf("  Kanboard URL: %s\n", user.KanboardURL)
	fmt.Printf("  Username: %s\n", user.KanboardUsername)
	fmt.Printf("  Created: %s\n", user.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Last Used: %s\n", user.LastUsed.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Token: [ENCRYPTED]\n")
}
