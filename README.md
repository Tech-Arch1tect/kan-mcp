# kan-mcp

A work-in-progress MCP (Model Context Protocol) server that provides access to Kanboard project management data.

## Features

- Two tools: `kanboard_overview` and `kanboard_tasks`
- Secure token storage with encryption
- Multi-user support with CLI management
- Both ~stdio and HTTP transport support

## Quick Start

### 1. Setup Environment

```bash
# Generate encryption key
export ENCRYPTION_KEY=$(openssl rand -hex 32)

# Set Kanboard URL (optional)
export DEFAULT_KANBOARD_URL="https://your-kanboard.example.com"
```

### 2. Register a User

```bash
go run ./cmd/server cli register -username your-kanboard-username
```

### 3. Run the Server

```bash
# stdio mode (for MCP clients)
go run ./cmd/server

# HTTP mode (for web access)
go run ./cmd/server -transport http
```

## CLI Commands

- `register` - Register a new user with Kanboard credentials
- `list` - List all registered users
- `show` - Show details for a specific user
- `delete` - Delete a user

## Environment Variables

- `ENCRYPTION_KEY` - 64-character hex string for encrypting tokens (required)
- `DEFAULT_KANBOARD_URL` - Default Kanboard instance URL
- `DATA_DIR` - Directory for user data storage (default: `./data`)
- `MCP_PORT` - HTTP server port (default: `8080`)

## Available Tools

- `kanboard_overview` - Get overview of accessible projects with columns, swimlanes, users, and optional task counts
- `kanboard_tasks` - Get detailed task information with filtering, sorting, and priority analysis
- `kanboard_priorities` - Analyse workload and provide priority recommendations

### `kanboard_tasks`

**Parameters:**
- `user_id` (required) - User ID for authentication
- `project_ids` (optional) - Comma-separated list of project IDs to filter by
- `assignee_ids` (optional) - Comma-separated list of assignee user IDs to filter by
- `status_filter` (optional) - Filter by 'active', 'completed', or 'all' (default: active)
- `due_date_start` (optional) - Filter by due date start (YYYY-MM-DD format)
- `due_date_end` (optional) - Filter by due date end (YYYY-MM-DD format)
- `include_overdue` (optional) - Include overdue tasks (default: false)
- `include_time_tracking` (optional) - Include time tracking information (default: true)
- `sort_by` (optional) - Sort by 'due_date', 'priority', or 'created' (default: due_date)
- `limit` (optional) - Maximum tasks to return (default: 20, max: 100/200)
- `summary_mode` (optional) - Return lightweight summaries vs full details (default: true)

### `kanboard_priorities`

**Parameters:**
- `user_id` (required) - User ID for authentication  
- `project_ids` (optional) - Comma-separated list of project IDs to filter by
- `time_horizon` (optional) - Time horizon for analysis: 'today', 'week', or 'month' (default: week)
- `include_recommendations` (optional) - Include priority recommendations (default: true)

## Building

```bash
go build -o kan-mcp ./cmd/server
```