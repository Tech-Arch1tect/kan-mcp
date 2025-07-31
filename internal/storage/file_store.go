package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tech-arch1tect/kan-mcp/internal/models"
)

type FileStore struct {
	dataDir string
	mutex   sync.RWMutex
}

func NewFileStore(dataDir string) (*FileStore, error) {

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	usersDir := filepath.Join(dataDir, "users")
	if err := os.MkdirAll(usersDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create users directory: %w", err)
	}

	return &FileStore{
		dataDir: dataDir,
	}, nil
}

func (fs *FileStore) SaveUser(user *models.User) error {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	userFile := filepath.Join(fs.dataDir, "users", user.UserID+".json")

	data, err := json.MarshalIndent(user, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}

	if err := os.WriteFile(userFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write user file: %w", err)
	}

	return nil
}

func (fs *FileStore) GetUser(userID string) (*models.User, error) {
	fs.mutex.RLock()
	defer fs.mutex.RUnlock()

	userFile := filepath.Join(fs.dataDir, "users", userID+".json")

	data, err := os.ReadFile(userFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to read user file: %w", err)
	}

	var user models.User
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}

	return &user, nil
}

func (fs *FileStore) DeleteUser(userID string) error {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	userFile := filepath.Join(fs.dataDir, "users", userID+".json")

	if err := os.Remove(userFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("user not found")
		}
		return fmt.Errorf("failed to delete user file: %w", err)
	}

	return nil
}

func (fs *FileStore) ListUsers() ([]*models.User, error) {
	fs.mutex.RLock()
	defer fs.mutex.RUnlock()

	usersDir := filepath.Join(fs.dataDir, "users")

	var users []*models.User

	err := filepath.Walk(usersDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("Warning: failed to read user file %s: %v\n", path, err)
			return nil
		}

		var user models.User
		if err := json.Unmarshal(data, &user); err != nil {
			fmt.Printf("Warning: failed to unmarshal user file %s: %v\n", path, err)
			return nil
		}

		users = append(users, &user)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	return users, nil
}
