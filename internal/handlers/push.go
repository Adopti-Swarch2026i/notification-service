package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"firebase.google.com/go/v4/messaging"
	"github.com/adopti/notification-service/internal/domain"
	msg "github.com/adopti/notification-service/internal/messaging"
	"github.com/adopti/notification-service/internal/repository"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// PushHandler envía notificaciones FCM. Los device tokens se persisten
// en Postgres (tabla device_tokens) para que sobrevivan al reinicio.
type PushHandler struct {
	client *messaging.Client
	repo   repository.NotificationRepository
	logger *zap.Logger
}

func NewPushHandler(msgClient *messaging.Client, repo repository.NotificationRepository, logger *zap.Logger) *PushHandler {
	return &PushHandler{
		client: msgClient,
		repo:   repo,
		logger: logger,
	}
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
	if err := h.repo.UpsertDeviceToken(c.Request.Context(), req.UserID, req.Token); err != nil {
		h.logger.Error("failed to persist device token", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist token"})
		return
	}
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

	if evt.RecipientID == "" {
		h.logger.Info("chat.message.sent without recipient (group chat?), skipping push")
		_ = h.persist(ctx, eventID, "chat.message.sent", "skipped_no_recipient", rawPayload, "")
		return nil
	}

	token, err := h.repo.GetDeviceToken(ctx, evt.RecipientID)
	if err != nil {
		return fmt.Errorf("failed to lookup device token: %w", err)
	}

	if token == "" {
		h.logger.Info("no device token found for recipient", zap.String("recipientId", evt.RecipientID))
		_ = h.persist(ctx, eventID, "chat.message.sent", "skipped_no_token", rawPayload, evt.RecipientID)
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

	if _, err := h.client.Send(ctx, message); err != nil {
		_ = h.persist(ctx, eventID, "chat.message.sent", "failed", rawPayload, evt.RecipientID)
		return fmt.Errorf("failed to send push: %w", err)
	}

	if err := h.persist(ctx, eventID, "chat.message.sent", "sent", rawPayload, evt.RecipientID); err != nil {
		h.logger.Error("failed to persist notification", zap.Error(err))
	}
	return nil
}

// HandlePetReportReunited dispara push al owner del reporte cuando se marca
// como reunited (plan §6.3). Si no hay token registrado, persiste como
// failed_no_token y no falla — el email ya cubre el caso.
func (h *PushHandler) HandlePetReportReunited(ctx context.Context, eventID string, evt msg.PetReportReunitedEvent, rawPayload string) error {
	return h.sendToUser(
		ctx,
		eventID,
		"pet.report.reunited",
		evt.OwnerID,
		"¡Tu mascota fue reunida! — Adopti",
		fmt.Sprintf("El reporte #%d se marcó como reunido.", evt.PetID),
		map[string]string{
			"petId":  fmt.Sprintf("%d", evt.PetID),
			"type":   "pet.report.reunited",
		},
		rawPayload,
	)
}

// HandleMatchFound dispara push al owner del reporte LOST cuando matching
// detecta una posible coincidencia (plan §6.3). El found-side no recibe
// push aquí: ese usuario ya tiene la mascota y no necesita alerta urgente.
func (h *PushHandler) HandleMatchFound(ctx context.Context, eventID string, evt msg.MatchFoundEvent, rawPayload string) error {
	if evt.LostOwnerID == "" {
		h.logger.Warn("match.found without lostOwnerId, skipping push", zap.String("eventId", eventID))
		_ = h.persist(ctx, eventID, "match.found", "skipped_no_owner", rawPayload, "")
		return nil
	}
	body := fmt.Sprintf(
		"Posible coincidencia con el reporte #%d (score %.0f%%). Revisa la app.",
		evt.FoundPetID,
		evt.Score*100,
	)
	return h.sendToUser(
		ctx,
		eventID,
		"match.found",
		evt.LostOwnerID,
		"¡Posible coincidencia! — Adopti",
		body,
		map[string]string{
			"lostPetId":  fmt.Sprintf("%d", evt.LostPetID),
			"foundPetId": fmt.Sprintf("%d", evt.FoundPetID),
			"type":       "match.found",
		},
		rawPayload,
	)
}

// sendToUser resuelve el device token del usuario y envía el push FCM,
// persistiendo el resultado en cualquier caso.
func (h *PushHandler) sendToUser(
	ctx context.Context,
	eventID, eventType, userID, title, body string,
	data map[string]string,
	rawPayload string,
) error {
	exists, err := h.repo.ExistsByEventIDAndChannel(ctx, eventID, "push")
	if err != nil {
		return fmt.Errorf("failed to check idempotency: %w", err)
	}
	if exists {
		h.logger.Info("skipping duplicate", zap.String("eventId", eventID), zap.String("channel", "push"))
		return nil
	}

	token, err := h.repo.GetDeviceToken(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to lookup device token: %w", err)
	}
	if token == "" {
		h.logger.Info("no device token registered for user, skipping push",
			zap.String("userId", userID), zap.String("eventType", eventType))
		_ = h.persist(ctx, eventID, eventType, "skipped_no_token", rawPayload, userID)
		return nil
	}
	if h.client == nil {
		_ = h.persist(ctx, eventID, eventType, "failed_no_client", rawPayload, userID)
		return fmt.Errorf("firebase client not initialized")
	}

	message := &messaging.Message{
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Token: token,
		Data:  data,
	}

	if _, err := h.client.Send(ctx, message); err != nil {
		_ = h.persist(ctx, eventID, eventType, "failed", rawPayload, userID)
		return fmt.Errorf("failed to send push: %w", err)
	}
	if err := h.persist(ctx, eventID, eventType, "sent", rawPayload, userID); err != nil {
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
