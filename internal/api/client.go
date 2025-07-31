package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/tech-arch1tect/kan-mcp/internal/models"
)

type Client struct {
	baseURL    string
	username   string
	token      string
	httpClient *http.Client
}

func NewClient(baseURL, username, token string) *Client {
	return &Client{
		baseURL:  baseURL,
		username: username,
		token:    token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) makeRequest(method string, params interface{}) (*models.JSONRPCResponse, error) {
	req := &models.JSONRPCRequest{
		JSONRpc: "2.0",
		Method:  method,
		ID:      1,
		Params:  params,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/jsonrpc.php", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(c.username + ":" + c.token))
	httpReq.Header.Set("Authorization", "Basic "+auth)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var jsonRPCResp models.JSONRPCResponse
	if err := json.Unmarshal(body, &jsonRPCResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if jsonRPCResp.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error: %s", jsonRPCResp.Error.Message)
	}

	return &jsonRPCResp, nil
}

func (c *Client) makeRawRequest(method string, params interface{}) (json.RawMessage, error) {
	resp, err := c.makeRequest(method, params)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	return json.RawMessage(data), nil
}

func (c *Client) GetMyProjectsRaw() (json.RawMessage, error) {
	return c.makeRawRequest("getMyProjects", nil)
}



func (c *Client) GetProjectUsers(projectID int) ([]models.KanboardUser, error) {
	resp, err := c.makeRequest("getProjectUsers", map[string]interface{}{"project_id": projectID})
	if err != nil {
		return nil, err
	}

	var users []models.KanboardUser

	if err := c.unmarshalResult(resp.Result, &users); err != nil {

		var userMap map[string]string
		if mapErr := c.unmarshalResult(resp.Result, &userMap); mapErr != nil {

			var interfaceMap map[string]interface{}
			if interfaceErr := c.unmarshalResult(resp.Result, &interfaceMap); interfaceErr != nil {
				return nil, fmt.Errorf("failed to unmarshal as array: %w, as string map: %w, as interface map: %w", err, mapErr, interfaceErr)
			}

			users = make([]models.KanboardUser, 0, len(interfaceMap))
			for userIDStr, value := range interfaceMap {
				userID := 0
				if id, parseErr := strconv.Atoi(userIDStr); parseErr == nil {
					userID = id
				}

				username := ""
				if str, ok := value.(string); ok {
					username = str
				} else if str, ok := value.(interface{}); ok {
					username = fmt.Sprintf("%v", str)
				}

				user := models.KanboardUser{
					ID:       userID,
					Username: username,
					Name:     username,
					Role:     "",
				}
				users = append(users, user)
			}
		} else {

			users = make([]models.KanboardUser, 0, len(userMap))
			for userIDStr, username := range userMap {
				userID := 0
				if id, parseErr := strconv.Atoi(userIDStr); parseErr == nil {
					userID = id
				}

				user := models.KanboardUser{
					ID:       userID,
					Username: username,
					Name:     username,
					Role:     "",
				}
				users = append(users, user)
			}
		}
	}

	return users, nil
}


func (c *Client) GetTasksByProject(projectID int) ([]models.Task, error) {
	resp, err := c.makeRequest("getAllTasks", map[string]interface{}{"project_id": projectID})
	if err != nil {
		return nil, err
	}

	var tasks []models.Task
	if err := c.unmarshalResult(resp.Result, &tasks); err != nil {
		return nil, err
	}

	return tasks, nil
}



func (c *Client) GetColumns(projectID int) ([]models.Column, error) {
	resp, err := c.makeRequest("getColumns", map[string]interface{}{"project_id": projectID})
	if err != nil {
		return nil, err
	}

	var columns []models.Column
	if err := c.unmarshalResult(resp.Result, &columns); err != nil {
		return nil, err
	}

	return columns, nil
}

func (c *Client) GetSwimlanes(projectID int) ([]models.Swimlane, error) {
	resp, err := c.makeRequest("getAllSwimlanes", map[string]interface{}{"project_id": projectID})
	if err != nil {
		return nil, err
	}

	var swimlanes []models.Swimlane
	if err := c.unmarshalResult(resp.Result, &swimlanes); err != nil {
		return nil, err
	}

	return swimlanes, nil
}

func (c *Client) GetMe() (*models.KanboardUser, error) {
	resp, err := c.makeRequest("getMe", nil)
	if err != nil {
		return nil, err
	}

	var user models.KanboardUser
	if err := c.unmarshalResult(resp.Result, &user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (c *Client) unmarshalResult(result interface{}, target interface{}) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return nil
}
