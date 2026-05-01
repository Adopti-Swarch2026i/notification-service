package messaging

import (
	"time"

	"github.com/rabbitmq/amqp091-go"
)

type PetReportCreatedEvent struct {
	PetID     string    `json:"petId"`
	OwnerID   string    `json:"ownerId"`
	Type      string    `json:"type"`
	Breed     string    `json:"breed"`
	Color     string    `json:"color"`
	Status    string    `json:"status"`
	City      string    `json:"city"`
	CreatedAt time.Time `json:"createdAt"`
}

type PetReportReunitedEvent struct {
	PetID      string    `json:"petId"`
	OwnerID    string    `json:"ownerId"`
	ReunitedAt time.Time `json:"reunitedAt"`
}

type MatchFoundEvent struct {
	LostPetID  string         `json:"lostPetId"`
	FoundPetID string         `json:"foundPetId"`
	Score      float64        `json:"score"`
	Criteria   map[string]any `json:"criteria"`
}

type ChatMessageSentEvent struct {
	ConversationID string    `json:"conversationId"`
	SenderID       string    `json:"senderId"`
	RecipientID    string    `json:"recipientId"`
	ContentPreview string    `json:"contentPreview"`
	Timestamp      time.Time `json:"timestamp"`
}

type EventEnvelope struct {
	Delivery       amqp091.Delivery
	EventID        string
	EventTimestamp time.Time
}
