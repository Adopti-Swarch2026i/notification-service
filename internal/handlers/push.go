package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/adopti/notification-service/internal/config"
	"github.com/adopti/notification-service/internal/domain"
	msg "github.com/adopti/notification-service/internal/messaging"
	"github.com/adopti/notification-service/internal/repository"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type PushHandler struct {
	client     *messaging.Client
	repo       repository.NotificationRepository
	logger     *zap.Logger
	deviceToks map[string]string
	mu         sync.RWMutex
}

func NewPushHandler(cfg *config.Config, repo repository.NotificationRepository, logger *zap.Logger) (*PushHandler, error) {
	app, err := firebase.NewApp(context.Background(), nil)
	if err != nil {
		logger.Warn("Failed to initialize firebase app, pushes will fail", zap.Error(err))
	}
	var client *messaging.Client
	if app != nil {
		client, err = app.Messaging(context.Background())
		if err != nil {
			logger.Warn("Failed to get messaging client", zap.Error(err))
		}
	}
	return &PushHandler{
		client:     client,
		repo:       repo,
		logger:     logger,
		deviceToks: make(map[string]string),
	}, nil
}

type TokenRequest struct {
	UserID string `json:"userId" binding:"required"`
	Token  string `json:"token" binding:"required"`
}

func (h *PushHandler) RegisterDeviceToken(c *gin.Context) {
	var req TokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.mu.Lock()
	h.deviceToks[req.UserID] = req.Token
	h.mu.Unlock()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *PushHandler) HandleChatMessageSent(ctx context.Context, eventID string, evt msg.ChatMessageSentEvent, rawPayload string) error {
	exists, err := h.repo.ExistsByEventIDAndChannel(ctx, eventID, "push")
	if err != nil {
		return fmt.Errorf("failed to check idempotency: %w", err)
	}
	if exists {
		h.logger.Info("skipping duplicate", zap.String("eventId", eventID), zap.String("channel", "push"))
		return nil
	}

	h.mu.RLock()
	token := h.deviceToks[evt.RecipientID]
	h.mu.RUnlock()

	if token == "" {
		h.logger.Info("no device token found for recipient", zap.String("recipientId", evt.RecipientID))
		_ = h.persist(ctx, eventID, "chat.message.sent", "failed_no_token", rawPayload, evt.RecipientID)
		return nil
	}

	if h.client == nil {
		_ = h.persist(ctx, eventID, "chat.message.sent", "failed_no_client", rawPayload, evt.RecipientID)
		return fmt.Errorf("firebase client not initialized")
	}

	message := &messaging.Message{
		Notification: &messaging.Notification{
			Title: "Nuevo mensaje — Adopti",
			Body:  evt.ContentPreview,
		},
		Token: token,
		Data: map[string]string{
			"conversationId": evt.ConversationID,
		},
	}

	_, err = h.client.Send(ctx, message)
	if err != nil {
		_ = h.persist(ctx, eventID, "chat.message.sent", "failed", rawPayload, evt.RecipientID)
		return fmt.Errorf("failed to send push: %w", err)
	}

	err = h.persist(ctx, eventID, "chat.message.sent", "sent", rawPayload, evt.RecipientID)
	if err != nil {
		h.logger.Error("failed to persist notification", zap.Error(err))
	}
	return nil
}

func (h *PushHandler) persist(ctx context.Context, eventID, eventType, status, payload, userID string) error {
	n := &domain.Notification{
		UserID:    userID,
		EventID:   eventID,
		EventType: eventType,
		Channel:   "push",
		Status:    status,
		Payload:   payload,
		CreatedAt: time.Now(),
	}
	return h.repo.Save(ctx, n)
}
