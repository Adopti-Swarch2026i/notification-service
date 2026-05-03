package messaging

import (
	"time"

	"github.com/rabbitmq/amqp091-go"
)

type PetReportCreatedEvent struct {
	PetID     int64     `json:"petId"`
	ReportID  int64     `json:"reportId"`
	OwnerID   string    `json:"ownerId"`
	Type      string    `json:"type"`
	Breed     string    `json:"breed"`
	Color     string    `json:"color"`
	Status    string    `json:"status"`
	City      string    `json:"city"`
	CreatedAt time.Time `json:"createdAt"`
}

type PetReportReunitedEvent struct {
	PetID      int64     `json:"petId"`
	OwnerID    string    `json:"ownerId"`
	ReunitedAt time.Time `json:"reunitedAt"`
}

type MatchFoundEvent struct {
	LostPetID    int64          `json:"lostPetId"`
	LostReportID int64          `json:"lostReportId"`
	LostOwnerID  string         `json:"lostOwnerId"`
	FoundPetID   int64          `json:"foundPetId"`
	FoundReportID int64         `json:"foundReportId"`
	FoundOwnerID string         `json:"foundOwnerId"`
	Score        float64        `json:"score"`
	Criteria     map[string]any `json:"criteria"`
	MatchedAt    time.Time      `json:"matchedAt"`
}

type ChatMessageSentEvent struct {
	ConversationID string    `json:"conversationId"`
	SenderID       string    `json:"senderId"`
	RecipientID    string    `json:"recipientId"`
	ContentPreview string    `json:"contentPreview"`
	Timestamp      int64     `json:"timestamp"`
}

type EventEnvelope struct {
	Delivery       amqp091.Delivery
	EventID        string
	EventTimestamp time.Time
}
