package domain

import (
	"time"

	"github.com/google/uuid"
)

type Notification struct {
	ID        uuid.UUID
	UserID    string
	EventID   string
	EventType string
	Channel   string
	Status    string
	Payload   string
	CreatedAt time.Time
}
