package handlers

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"firebase.google.com/go/v4/auth"
	"github.com/adopti/notification-service/internal/config"
	"github.com/adopti/notification-service/internal/domain"
	"github.com/adopti/notification-service/internal/messaging"
	"github.com/adopti/notification-service/internal/repository"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
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

	pet := describePet(evt.Type, evt.Breed, evt.Color)
	statusLabel := statusLabel(evt.Status)
	subject := fmt.Sprintf("Reporte recibido — %s", pet)

	intro := fmt.Sprintf(
		"Recibimos tu reporte de %s. Estamos empezando a buscar coincidencias dentro de la comunidad Adopti.",
		statusLabel,
	)
	details := []detail{
		{"Mascota", pet},
		{"Estado", statusLabel},
		{"Ciudad", fallback(evt.City, "no especificada")},
		{"Fecha", evt.CreatedAt.Format("02 ene 2006, 15:04")},
		{"Reporte", fmt.Sprintf("#%d", evt.ReportID)},
	}
	htmlBody, plainBody := buildEmailBodies("¡Tu reporte fue recibido!", intro, details, "")

	toEmail := h.resolveOwnerEmail(ctx, evt.OwnerID)
	return h.send(ctx, eventID, "pet.report.created", evt.OwnerID, rawPayload, toEmail, subject, plainBody, htmlBody)
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

	subject := "¡Tu mascota fue reunida! 🎉 — Adopti"
	intro := "Buenas noticias: tu reporte fue marcado como reunido. Gracias por mantener la comunidad informada."
	details := []detail{
		{"Reporte", fmt.Sprintf("#%d", evt.PetID)},
		{"Reunida el", evt.ReunitedAt.Format("02 ene 2006, 15:04")},
	}
	htmlBody, plainBody := buildEmailBodies("¡Mascota reunida!", intro, details, "")

	toEmail := h.resolveOwnerEmail(ctx, evt.OwnerID)
	return h.send(ctx, eventID, "pet.report.reunited", evt.OwnerID, rawPayload, toEmail, subject, plainBody, htmlBody)
}

func (h *EmailHandler) HandleMatchFound(ctx context.Context, eventID string, evt messaging.MatchFoundEvent, rawPayload string) error {
	exists, err := h.repo.ExistsByEventIDAndChannel(ctx, eventID, "email")
	if err != nil {
		return fmt.Errorf("failed to check idempotency: %w", err)
	}
	if exists {
		h.logger.Info("skipping duplicate", zap.String("eventId", eventID), zap.String("channel", "email"))
		return nil
	}

	subject := "¡Posible coincidencia encontrada! — Adopti"
	intro := "Detectamos un reporte que podría coincidir con tu mascota perdida. Te recomendamos revisarlo en la app cuanto antes."

	species := stringFromCriteria(evt.Criteria, "species")
	city := stringFromCriteria(evt.Criteria, "city")
	breed := stringFromCriteria(evt.Criteria, "breed")
	color := stringFromCriteria(evt.Criteria, "color")

	details := []detail{
		{"Reporte perdido", fmt.Sprintf("#%d", evt.LostPetID)},
		{"Reporte encontrado", fmt.Sprintf("#%d", evt.FoundPetID)},
		{"Compatibilidad", fmt.Sprintf("%.0f%%", evt.Score*100)},
	}
	if species != "" {
		details = append(details, detail{"Especie", species})
	}
	if breed != "" {
		details = append(details, detail{"Raza", breed})
	}
	if color != "" {
		details = append(details, detail{"Color", color})
	}
	if city != "" {
		details = append(details, detail{"Ciudad", city})
	}

	htmlBody, plainBody := buildEmailBodies("Posible coincidencia", intro, details, "Abre Adopti y revisa el reporte encontrado para comparar fotos y contactar.")

	toEmail := h.resolveOwnerEmail(ctx, evt.LostOwnerID)
	userID := evt.LostOwnerID
	if userID == "" {
		userID = fmt.Sprintf("lost:%d", evt.LostPetID)
	}
	return h.send(ctx, eventID, "match.found", userID, rawPayload, toEmail, subject, plainBody, htmlBody)
}

// ── Helpers ────────────────────────────────────────────────────────────

func (h *EmailHandler) resolveOwnerEmail(ctx context.Context, uid string) string {
	if h.authClient != nil && uid != "" {
		u, err := h.authClient.GetUser(ctx, uid)
		if err == nil && u.Email != "" {
			return u.Email
		}
		h.logger.Warn("failed to resolve email from Firebase, using fallback",
			zap.Error(err), zap.String("uid", uid))
	}
	if h.testEmail != "" {
		return h.testEmail
	}
	return "test@example.com"
}

func (h *EmailHandler) send(ctx context.Context, eventID, eventType, userID, rawPayload, toEmail, subject, plain, htmlBody string) error {
	from := mail.NewEmail("Adopti Notifications", h.fromEmail)
	to := mail.NewEmail("Adopti User", toEmail)
	message := mail.NewSingleEmail(from, subject, to, plain, htmlBody)

	client := sendgrid.NewSendClient(h.apiKey)
	response, err := client.Send(message)
	if err != nil || (response != nil && response.StatusCode >= 400) {
		_ = h.persist(ctx, eventID, eventType, "failed", rawPayload, userID)
		if err != nil {
			return fmt.Errorf("failed to send email: %w", err)
		}
		return fmt.Errorf("sendgrid returned status %d: %s", response.StatusCode, response.Body)
	}
	if err := h.persist(ctx, eventID, eventType, "sent", rawPayload, userID); err != nil {
		h.logger.Error("failed to persist notification", zap.Error(err))
	}
	return nil
}

type detail struct {
	Label string
	Value string
}

// buildEmailBodies arma el cuerpo HTML y el equivalente en texto plano.
// El texto plano es importante: Gmail lo usa para el preview de la inbox.
func buildEmailBodies(title, intro string, details []detail, footerCTA string) (htmlBody, plain string) {
	var rows strings.Builder
	for _, d := range details {
		rows.WriteString(fmt.Sprintf(
			`<tr><td style="padding:6px 12px;color:#6b7280;font-size:13px;">%s</td>`+
				`<td style="padding:6px 12px;color:#111827;font-size:14px;font-weight:500;">%s</td></tr>`,
			html.EscapeString(d.Label), html.EscapeString(d.Value),
		))
	}

	cta := ""
	if footerCTA != "" {
		cta = fmt.Sprintf(
			`<p style="margin:20px 0 0;color:#374151;font-size:14px;line-height:1.5;">%s</p>`,
			html.EscapeString(footerCTA),
		)
	}

	htmlBody = fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:0;background:#f3f4f6;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;">
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#f3f4f6;padding:32px 16px;">
    <tr><td align="center">
      <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="max-width:560px;background:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 1px 3px rgba(0,0,0,0.08);">
        <tr><td style="background:#0f172a;padding:20px 28px;color:#ffffff;font-weight:600;font-size:18px;letter-spacing:0.3px;">Adopti</td></tr>
        <tr><td style="padding:28px;">
          <h1 style="margin:0 0 12px;color:#111827;font-size:22px;font-weight:600;">%s</h1>
          <p style="margin:0 0 20px;color:#374151;font-size:15px;line-height:1.6;">%s</p>
          <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="border:1px solid #e5e7eb;border-radius:8px;border-collapse:separate;">
            %s
          </table>
          %s
        </td></tr>
        <tr><td style="padding:16px 28px;background:#f9fafb;color:#9ca3af;font-size:12px;text-align:center;">Recibiste este correo porque tienes una cuenta en Adopti.</td></tr>
      </table>
    </td></tr>
  </table>
</body></html>`,
		html.EscapeString(title),
		html.EscapeString(intro),
		rows.String(),
		cta,
	)

	var p strings.Builder
	p.WriteString(title)
	p.WriteString("\n\n")
	p.WriteString(intro)
	p.WriteString("\n\n")
	for _, d := range details {
		p.WriteString(d.Label)
		p.WriteString(": ")
		p.WriteString(d.Value)
		p.WriteString("\n")
	}
	if footerCTA != "" {
		p.WriteString("\n")
		p.WriteString(footerCTA)
		p.WriteString("\n")
	}
	plain = p.String()
	return
}

// describePet construye una descripción legible omitiendo campos vacíos.
// Devuelve por ejemplo "perro labrador negro", "gato negro" o "mascota".
func describePet(species, breed, color string) string {
	parts := joinNonEmpty(" ", species, breed, color)
	if parts == "" {
		return "mascota"
	}
	return parts
}

func joinNonEmpty(sep string, parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, sep)
}

func fallback(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

func statusLabel(status string) string {
	switch strings.ToLower(status) {
	case "lost":
		return "mascota perdida"
	case "found":
		return "mascota encontrada"
	case "reunited":
		return "mascota reunida"
	default:
		return status
	}
}

func stringFromCriteria(criteria map[string]any, key string) string {
	if v, ok := criteria[key].(string); ok {
		return v
	}
	return ""
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
