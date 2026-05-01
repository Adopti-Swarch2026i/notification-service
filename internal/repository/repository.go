package repository

import (
	"context"

	"github.com/adopti/notification-service/internal/domain"
)

type NotificationRepository interface {
	Save(ctx context.Context, n *domain.Notification) error
	FindByUserID(ctx context.Context, userID, status string, limit, offset int) ([]*domain.Notification, error)
	ExistsByEventIDAndChannel(ctx context.Context, eventID, channel string) (bool, error)
}
