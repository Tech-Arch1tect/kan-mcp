package models

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type KanboardBool bool

func (kb *KanboardBool) UnmarshalJSON(data []byte) error {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}

	switch value := v.(type) {
	case bool:
		*kb = KanboardBool(value)
	case float64:
		*kb = KanboardBool(value != 0)
	case string:
		*kb = KanboardBool(value == "1" || value == "true")
	default:
		*kb = false
	}
	return nil
}

type KanboardString string

func (ks *KanboardString) UnmarshalJSON(data []byte) error {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}

	switch value := v.(type) {
	case string:
		*ks = KanboardString(value)
	case float64:
		*ks = KanboardString(strconv.FormatFloat(value, 'f', -1, 64))
	case int:
		*ks = KanboardString(strconv.Itoa(value))
	case nil:
		*ks = KanboardString("")
	default:
		*ks = KanboardString(fmt.Sprintf("%v", value))
	}
	return nil
}

type KanboardTime struct {
	time.Time
}

func (kt *KanboardTime) UnmarshalJSON(data []byte) error {

	str := string(data)
	if len(str) >= 2 && str[0] == '"' && str[len(str)-1] == '"' {
		str = str[1 : len(str)-1]
	}

	if str == "" || str == "0" {
		kt.Time = time.Time{}
		return nil
	}

	if timestamp, err := strconv.ParseInt(str, 10, 64); err == nil {
		kt.Time = time.Unix(timestamp, 0).UTC()
		return nil
	}

	if t, err := time.Parse(time.RFC3339, str); err == nil {
		kt.Time = t
		return nil
	}

	if t, err := time.Parse("2006-01-02", str); err == nil {
		kt.Time = t
		return nil
	}

	kt.Time = time.Time{}
	return nil
}


type Task struct {
	ID                  int          `json:"id"`
	Title               string       `json:"title"`
	Description         string       `json:"description"`
	DateCreation        KanboardTime `json:"date_creation"`
	ColorID             string       `json:"color_id"`
	ProjectID           int          `json:"project_id"`
	ColumnID            int          `json:"column_id"`
	OwnerID             int          `json:"owner_id"`
	Position            int          `json:"position"`
	IsActive            KanboardBool `json:"is_active"`
	DateCompleted       KanboardTime `json:"date_completed"`
	Score               int          `json:"score"`
	DateDue             KanboardTime `json:"date_due"`
	CategoryID          int          `json:"category_id"`
	CreatorID           int          `json:"creator_id"`
	DateModified        KanboardTime `json:"date_modification"`
	Reference           string       `json:"reference"`
	DateStarted         KanboardTime `json:"date_started"`
	TimeSpent           float64      `json:"time_spent"`
	TimeEstimated       float64      `json:"time_estimated"`
	SwimlaneID          int          `json:"swimlane_id"`
	DateMoved           KanboardTime `json:"date_moved"`
	RecurrenceStatus    int          `json:"recurrence_status"`
	RecurrenceTrigger   int          `json:"recurrence_trigger"`
	RecurrenceFactor    int          `json:"recurrence_factor"`
	RecurrenceTimeframe int          `json:"recurrence_timeframe"`
	RecurrenceBasedate  int          `json:"recurrence_basedate"`
	RecurrenceParent    int          `json:"recurrence_parent"`
	RecurrenceChild     int          `json:"recurrence_child"`
	URL                 string       `json:"url"`
}

type Column struct {
	ID              int          `json:"id"`
	Title           string       `json:"title"`
	Position        int          `json:"position"`
	ProjectID       int          `json:"project_id"`
	TaskLimit       int          `json:"task_limit"`
	Description     string       `json:"description"`
	HideInDashboard KanboardBool `json:"hide_in_dashboard"`
}

type Swimlane struct {
	ID          int          `json:"id"`
	Name        string       `json:"name"`
	Position    int          `json:"position"`
	IsActive    KanboardBool `json:"is_active"`
	ProjectID   int          `json:"project_id"`
	Description string       `json:"description"`
}


type KanboardUser struct {
	ID                   int            `json:"id"`
	Username             string         `json:"username"`
	Password             string         `json:"password"`
	Role                 string         `json:"role"`
	IsLdapUser           KanboardBool   `json:"is_ldap_user"`
	Name                 string         `json:"name"`
	Email                string         `json:"email"`
	GoogleID             KanboardString `json:"google_id"`
	GithubID             KanboardString `json:"github_id"`
	NotificationsEnabled KanboardBool   `json:"notifications_enabled"`
	Timezone             string         `json:"timezone"`
	Language             string         `json:"language"`
	DisableLoginForm     KanboardBool   `json:"disable_login_form"`
	TwofactorActivated   KanboardBool   `json:"twofactor_activated"`
	TwofactorSecret      string         `json:"twofactor_secret"`
	Token                string         `json:"token"`
	NotificationTypes    KanboardString `json:"notifications_filter"`
	NotificationProjects KanboardString `json:"nb_failed_login"`
	LockExpirationDate   KanboardString `json:"lock_expiration_date"`
	GLAvatarURL          KanboardString `json:"gitlab_id"`
	ApiAccessToken       string         `json:"api_access_token"`
	AvatarPath           string         `json:"avatar_path"`
}
