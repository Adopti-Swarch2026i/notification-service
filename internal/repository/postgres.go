package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adopti/notification-service/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepo struct {
	writePool *pgxpool.Pool
	readPool  *pgxpool.Pool
}

func NewPostgresRepo(ctx context.Context, writeDSN, readDSN string) (*PostgresRepo, error) {
	writePool, err := pgxpool.New(ctx, writeDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres (write): %w", err)
	}

	if err := writePool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping postgres (write): %w", err)
	}

	if err := runMigrations(ctx, writePool, "migrations"); err != nil {
		return nil, err
	}

	// Fallback: si no hay DSN del replica, usa el master para ambos
	if readDSN == "" {
		readDSN = writeDSN
	}
	readPool, err := pgxpool.New(ctx, readDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres (read): %w", err)
	}

	return &PostgresRepo{writePool: writePool, readPool: readPool}, nil
}

// runMigrations aplica todos los archivos *.sql del directorio en orden
// lexicográfico. Cada archivo debe ser idempotente (CREATE TABLE IF NOT EXISTS).
func runMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read migrations dir: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		sqlBytes, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", name, err)
		}
	}
	return nil
}

func (r *PostgresRepo) Save(ctx context.Context, n *domain.Notification) error {
	// Upsert por (event_id, channel): un retry exitoso tras una falla
	// transitoria actualiza el status, no se descarta. Combinado con
	// ExistsByEventIDAndChannel filtrando por status='sent' (events.md §2.1),
	// la idempotencia bloquea solo los envíos confirmados, no los fallidos.
	query := `
		INSERT INTO notifications (user_id, event_id, event_type, channel, status, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (event_id, channel) DO UPDATE
		SET status = EXCLUDED.status,
		    payload = EXCLUDED.payload,
		    created_at = EXCLUDED.created_at
		RETURNING id
	`
	err := r.writePool.QueryRow(ctx, query,
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

func (r *PostgresRepo) FindByUserID(ctx context.Context, userID, status string, limit, offset int) ([]*domain.Notification, error) {
	query := `
		SELECT id, user_id, event_id, event_type, channel, status, payload, created_at 
		FROM notifications
		WHERE user_id = $1`
	
	args := []any{userID}

	if status != "" {
		query += ` AND status = $2 ORDER BY created_at DESC LIMIT $3 OFFSET $4`
		args = append(args, status, limit, offset)
	} else {
		query += ` ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		args = append(args, limit, offset)
	}

	rows, err := r.readPool.Query(ctx, query, args...)
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
	// Solo bloqueamos si el envío anterior fue exitoso ('sent'). Filas en
	// estado 'failed*' permiten reintento — se actualizan vía Save upsert.
	query := `SELECT EXISTS(
		SELECT 1 FROM notifications
		WHERE event_id = $1 AND channel = $2 AND status = 'sent'
	)`
	var exists bool
	err := r.readPool.QueryRow(ctx, query, eventID, channel).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check existence: %w", err)
	}
	return exists, nil
}

func (r *PostgresRepo) UpsertDeviceToken(ctx context.Context, userID, token string) error {
	query := `
		INSERT INTO device_tokens (user_id, token, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (user_id) DO UPDATE
		SET token = EXCLUDED.token, updated_at = now()
	`
	if _, err := r.writePool.Exec(ctx, query, userID, token); err != nil {
		return fmt.Errorf("failed to upsert device token: %w", err)
	}
	return nil
}

func (r *PostgresRepo) GetDeviceToken(ctx context.Context, userID string) (string, error) {
	var token string
	err := r.readPool.QueryRow(ctx,
		`SELECT token FROM device_tokens WHERE user_id = $1`, userID,
	).Scan(&token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get device token: %w", err)
	}
	return token, nil
}
