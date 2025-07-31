package models

import (
	"time"
)

type User struct {
	UserID           string    `json:"user_id"`
	KanboardURL      string    `json:"kanboard_url,omitempty"`
	KanboardUsername string    `json:"kanboard_username"`
	KanboardToken    string    `json:"kanboard_token"`
	CreatedAt        time.Time `json:"created_at"`
	LastUsed         time.Time `json:"last_used"`
}

type UserConfig struct {
	DefaultKanboardURL string
	EncryptionKey      []byte
}
