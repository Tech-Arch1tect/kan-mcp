package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/tech-arch1tect/kan-mcp/internal/models"
	"github.com/tech-arch1tect/kan-mcp/pkg/encryption"
)

type AuthManager struct {
	encryptor *encryption.Encryptor
	userStore UserStore
}

type UserStore interface {
	SaveUser(user *models.User) error
	GetUser(userID string) (*models.User, error)
	DeleteUser(userID string) error
	ListUsers() ([]*models.User, error)
}

func NewAuthManager(encryptionKey []byte, userStore UserStore) (*AuthManager, error) {
	encryptor, err := encryption.NewEncryptor(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create encryptor: %w", err)
	}

	return &AuthManager{
		encryptor: encryptor,
		userStore: userStore,
	}, nil
}

func (a *AuthManager) RegisterUser(kanboardURL, kanboardUsername, kanboardToken string) (*models.User, error) {

	userID, err := a.generateUserID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate user ID: %w", err)
	}

	encryptedToken, err := a.encryptor.Encrypt(kanboardToken)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt token: %w", err)
	}

	user := &models.User{
		UserID:           userID,
		KanboardURL:      kanboardURL,
		KanboardUsername: kanboardUsername,
		KanboardToken:    encryptedToken,
		CreatedAt:        time.Now(),
		LastUsed:         time.Now(),
	}

	if err := a.userStore.SaveUser(user); err != nil {
		return nil, fmt.Errorf("failed to save user: %w", err)
	}

	return user, nil
}

func (a *AuthManager) AuthenticateUser(userID string) (*models.User, error) {
	user, err := a.userStore.GetUser(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	user.LastUsed = time.Now()
	if err := a.userStore.SaveUser(user); err != nil {
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	return user, nil
}

func (a *AuthManager) GetDecryptedToken(user *models.User) (string, error) {
	token, err := a.encryptor.Decrypt(user.KanboardToken)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt token: %w", err)
	}
	return token, nil
}

func (a *AuthManager) DeleteUser(userID string) error {
	return a.userStore.DeleteUser(userID)
}

func (a *AuthManager) ListUsers() ([]*models.User, error) {
	return a.userStore.ListUsers()
}

func (a *AuthManager) generateUserID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
