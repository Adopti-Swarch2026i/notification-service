package repository

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/adopti/notification-service/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepo struct {
	pool *pgxpool.Pool
}

func NewPostgresRepo(ctx context.Context, dsn string) (*PostgresRepo, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	migrationSQL, err := os.ReadFile("migrations/001_create_notifications.sql")
	if err != nil {
		return nil, fmt.Errorf("failed to read migration file: %w", err)
	}

	_, err = pool.Exec(ctx, string(migrationSQL))
	if err != nil {
		return nil, fmt.Errorf("failed to execute migration: %w", err)
	}

	return &PostgresRepo{pool: pool}, nil
}

func (r *PostgresRepo) Save(ctx context.Context, n *domain.Notification) error {
	query := `
		INSERT INTO notifications (user_id, event_id, event_type, channel, status, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (event_id, channel) DO NOTHING
		RETURNING id
	`
	err := r.pool.QueryRow(ctx, query,
		n.UserID, n.EventID, n.EventType, n.Channel, n.Status, n.Payload, n.CreatedAt,
	).Scan(&n.ID)
	
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("failed to save notification: %w", err)
	}

	return nil
}

func (r *PostgresRepo) FindByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.Notification, error) {
	query := `
		SELECT id, user_id, event_id, event_type, channel, status, payload, created_at 
		FROM notifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query notifications: %w", err)
	}
	defer rows.Close()

	var notifications []*domain.Notification
	for rows.Next() {
		var n domain.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.EventID, &n.EventType, &n.Channel, &n.Status, &n.Payload, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan notification row: %w", err)
		}
		notifications = append(notifications, &n)
	}

	return notifications, rows.Err()
}

func (r *PostgresRepo) ExistsByEventIDAndChannel(ctx context.Context, eventID, channel string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM notifications WHERE event_id = $1 AND channel = $2)`
	var exists bool
	err := r.pool.QueryRow(ctx, query, eventID, channel).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check existence: %w", err)
	}
	return exists, nil
}
