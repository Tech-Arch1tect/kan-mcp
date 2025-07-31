# kan-mcp

A work-in-progress MCP (Model Context Protocol) server that provides access to Kanboard project management data.

## Features

- Single tool: `kanboard_overview` - get project overviews with boards and task counts
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

## Available Tool

- `kanboard_overview` - Get overview of accessible projects with columns, swimlanes, users, and optional task counts

## Building

```bash
go build -o kan-mcp ./cmd/server
```