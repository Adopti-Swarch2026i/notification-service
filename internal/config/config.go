package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port                string
	RabbitMQURL         string
	PostgresDSN         string
	SendGridAPIKey      string
	SendGridFromEmail   string
	FirebaseCredentials string
	LogLevel            string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                os.Getenv("PORT"),
		RabbitMQURL:         os.Getenv("RABBITMQ_URL"),
		PostgresDSN:         os.Getenv("POSTGRES_DSN"),
		SendGridAPIKey:      os.Getenv("SENDGRID_API_KEY"),
		SendGridFromEmail:   os.Getenv("SENDGRID_FROM_EMAIL"),
		FirebaseCredentials: os.Getenv("FIREBASE_CREDENTIALS"),
		LogLevel:            os.Getenv("LOG_LEVEL"),
	}

	if cfg.Port == "" {
		return nil, fmt.Errorf("PORT is required")
	}
	if cfg.RabbitMQURL == "" {
		return nil, fmt.Errorf("RABBITMQ_URL is required")
	}
	if cfg.PostgresDSN == "" {
		return nil, fmt.Errorf("POSTGRES_DSN is required")
	}
	if cfg.SendGridAPIKey == "" {
		return nil, fmt.Errorf("SENDGRID_API_KEY is required")
	}
	if cfg.SendGridFromEmail == "" {
		return nil, fmt.Errorf("SENDGRID_FROM_EMAIL is required")
	}
	if cfg.FirebaseCredentials == "" {
		return nil, fmt.Errorf("FIREBASE_CREDENTIALS is required")
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	return cfg, nil
}
