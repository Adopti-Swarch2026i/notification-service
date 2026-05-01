package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/adopti/notification-service/internal/config"
	"github.com/adopti/notification-service/internal/domain"
	"github.com/adopti/notification-service/internal/messaging"
	"github.com/adopti/notification-service/internal/repository"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"firebase.google.com/go/v4/auth"
	"go.uber.org/zap"
)

type EmailHandler struct {
	apiKey     string
	fromEmail  string
	testEmail  string
	authClient *auth.Client
	repo       repository.NotificationRepository
	logger     *zap.Logger
}

func NewEmailHandler(cfg *config.Config, authClient *auth.Client, repo repository.NotificationRepository, logger *zap.Logger) *EmailHandler {
	return &EmailHandler{
		apiKey:     cfg.SendGridAPIKey,
		fromEmail:  cfg.SendGridFromEmail,
		testEmail:  cfg.TestEmail,
		authClient: authClient,
		repo:       repo,
		logger:     logger,
	}
}

func (h *EmailHandler) HandlePetReportCreated(ctx context.Context, eventID string, evt messaging.PetReportCreatedEvent, rawPayload string) error {
	exists, err := h.repo.ExistsByEventIDAndChannel(ctx, eventID, "email")
	if err != nil {
		return fmt.Errorf("failed to check idempotency: %w", err)
	}
	if exists {
		h.logger.Info("skipping duplicate", zap.String("eventId", eventID), zap.String("channel", "email"))
		return nil
	}

	from := mail.NewEmail("Adopti Notifications", h.fromEmail)
	subject := "Reporte creado — Adopti"
	
	toEmail := h.testEmail
	if h.authClient != nil {
		u, err := h.authClient.GetUser(ctx, evt.OwnerID)
		if err == nil && u.Email != "" {
			toEmail = u.Email
		} else {
			h.logger.Warn("Failed to resolve user email from Firebase, using fallback", zap.Error(err), zap.String("uid", evt.OwnerID))
		}
	}
	if toEmail == "" {
		toEmail = "test@example.com"
	}
	
	to := mail.NewEmail("Adopti User", toEmail)
	content := fmt.Sprintf("Tu reporte para %s %s en %s ha sido recibido.", evt.Breed, evt.Color, evt.City)
	
	message := mail.NewSingleEmail(from, subject, to, content, content)
	client := sendgrid.NewSendClient(h.apiKey)
	
	response, err := client.Send(message)
	if err != nil || response.StatusCode >= 400 {
		_ = h.persist(ctx, eventID, "pet.report.created", "failed", rawPayload, evt.OwnerID)
		if err != nil {
			return fmt.Errorf("failed to send email: %w", err)
		}
		if response != nil {
			return fmt.Errorf("sendgrid returned status %d: %s", response.StatusCode, response.Body)
		}
		return fmt.Errorf("failed to send email: %w", err)
	}

	err = h.persist(ctx, eventID, "pet.report.created", "sent", rawPayload, evt.OwnerID)
	if err != nil {
		h.logger.Error("failed to persist notification", zap.Error(err))
	}

	return nil
}

func (h *EmailHandler) HandlePetReportReunited(ctx context.Context, eventID string, evt messaging.PetReportReunitedEvent, rawPayload string) error {
	exists, err := h.repo.ExistsByEventIDAndChannel(ctx, eventID, "email")
	if err != nil {
		return fmt.Errorf("failed to check idempotency: %w", err)
	}
	if exists {
		h.logger.Info("skipping duplicate", zap.String("eventId", eventID), zap.String("channel", "email"))
		return nil
	}

	from := mail.NewEmail("Adopti Notifications", h.fromEmail)
	subject := "¡Tu mascota fue reunida! — Adopti"
	
	toEmail := h.testEmail
	if h.authClient != nil {
		u, err := h.authClient.GetUser(ctx, evt.OwnerID)
		if err == nil && u.Email != "" {
			toEmail = u.Email
		} else {
			h.logger.Warn("Failed to resolve user email from Firebase, using fallback", zap.Error(err), zap.String("uid", evt.OwnerID))
		}
	}
	if toEmail == "" {
		toEmail = "test@example.com"
	}

	to := mail.NewEmail("Adopti User", toEmail)
	content := fmt.Sprintf("El reporte %d fue marcado como reunido el %s.", evt.PetID, evt.ReunitedAt.Format(time.RFC3339))

	message := mail.NewSingleEmail(from, subject, to, content, content)
	client := sendgrid.NewSendClient(h.apiKey)
	
	response, err := client.Send(message)
	if err != nil || response.StatusCode >= 400 {
		_ = h.persist(ctx, eventID, "pet.report.reunited", "failed", rawPayload, evt.OwnerID)
		if err != nil {
			return fmt.Errorf("failed to send email: %w", err)
		}
		if response != nil {
			return fmt.Errorf("sendgrid returned status %d: %s", response.StatusCode, response.Body)
		}
		return fmt.Errorf("failed to send email: %w", err)
	}

	err = h.persist(ctx, eventID, "pet.report.reunited", "sent", rawPayload, evt.OwnerID)
	if err != nil {
		h.logger.Error("failed to persist notification", zap.Error(err))
	}

	return nil
}

func (h *EmailHandler) persist(ctx context.Context, eventID, eventType, status, payload, userID string) error {
	n := &domain.Notification{
		UserID:    userID,
		EventID:   eventID,
		EventType: eventType,
		Channel:   "email",
		Status:    status,
		Payload:   payload,
		CreatedAt: time.Now(),
	}
	return h.repo.Save(ctx, n)
}
